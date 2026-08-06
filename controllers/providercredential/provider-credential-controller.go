// Copyright Contributors to the Open Cluster Management project.

package providercredential

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const CredentialHash = "credential-hash" //#nosec G101
const ProviderTypeLabel = "cluster.open-cluster-management.io/type"
const copiedFromNamespaceLabel = "cluster.open-cluster-management.io/copiedFromNamespace"
const copiedFromNameLabel = "cluster.open-cluster-management.io/copiedFromSecretName"
const CredentialLabel = "cluster.open-cluster-management.io/credentials" //#nosec G101

// managedClusterGVK identifies the cluster-scoped ManagedCluster resource
// (group cluster.open-cluster-management.io, version v1). It is looked up
// via unstructured.Unstructured rather than the typed
// open-cluster-management.io/api client to avoid taking on that module as a
// dependency here.
var managedClusterGVK = schema.GroupVersionKind{
	Group:   "cluster.open-cluster-management.io",
	Version: "v1",
	Kind:    "ManagedCluster",
}

// managedClusterJoinedCondition is the status.conditions[].type value the
// hub's registration controller sets on a ManagedCluster once the agent has
// completed the CA-signed, CSR-approved double opt-in join handshake (see
// open-cluster-management.io/api/cluster/v1: ManagedClusterConditionJoined).
// It is not attacker-assertible: creating a ManagedCluster requires
// cluster-scoped RBAC normally reserved for hub admins/registration
// components, and even then only the hub's own controller can flip this
// condition to True as the result of a real agent registering.
const managedClusterJoinedCondition = "ManagedClusterJoined"

// UnauthorizedCredentialCopyEventReason is the Warning Event reason recorded
// on a child secret when it is skipped because its namespace is not a
// Joined ManagedCluster. It gives operators visibility (kubectl describe /
// events on the child) and doubles as a detection signal for exactly the
// attack this gate defends against - anything watching for Warning events
// with this reason cluster-wide should treat it as a potential credential
// theft attempt, not just a benign misconfiguration.
const UnauthorizedCredentialCopyEventReason = "UnauthorizedCredentialCopy"

const rhvConfigTemplate = `ovirt_url: %s
ovirt_username: %s
ovirt_password: %s
ovirt_ca_bundle: |+
  %s`

var hash = sha256.New()

// ProviderCredentialSecretReconciler reconciles a Provider secret
type ProviderCredentialSecretReconciler struct {
	client.Client
	APIReader client.Reader
	Log       logr.Logger
	Scheme    *runtime.Scheme
	Recorder  record.EventRecorder
}

func generateHash(valueBytes []byte) ([]byte, error) {

	hash.Reset()
	_, err := hash.Write(valueBytes)

	return hash.Sum(nil), err
}

// isJoinedManagedClusterNamespace returns true iff "namespace" is the name
// of a ManagedCluster that has joined the hub.
//
// Copied provider-credential secrets are expected to live in a managed
// cluster's namespace, which the hub creates and controls; a namespace name
// only becomes a real, Joined ManagedCluster as the outcome of the
// registration double opt-in (spec.hubAcceptsClient set by a hub
// cluster-admin, followed by a CSR-approved agent handshake). A tenant
// confined to a namespace they control cannot manufacture a same-named
// ManagedCluster in the Joined state, so this check establishes that
// "namespace" is hub-sanctioned rather than attacker-staged. Any lookup
// error (including NotFound) fails closed.
func isJoinedManagedClusterNamespace(ctx context.Context, reader client.Reader, namespace string) bool {
	mc := &unstructured.Unstructured{}
	mc.SetGroupVersionKind(managedClusterGVK)

	if err := reader.Get(ctx, types.NamespacedName{Name: namespace}, mc); err != nil {
		return false
	}

	conditions, found, err := unstructured.NestedSlice(mc.Object, "status", "conditions")
	if err != nil || !found {
		return false
	}

	for _, c := range conditions {
		condition, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		if condition["type"] == managedClusterJoinedCondition && condition["status"] == "True" {
			return true
		}
	}

	return false
}

func (r *ProviderCredentialSecretReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {

	log := r.Log.WithValues("ProviderCredentialSecretReconciler", req.NamespacedName)

	var secret corev1.Secret
	if err := r.Get(ctx, req.NamespacedName, &secret); err != nil {
		log.V(0).Info("Resource deleted")
		return ctrl.Result{}, err
	}

	log.V(1).Info("Reconcile secret")

	// This is the hash for the original secret.Data
	var originalHash []byte
	a := secret.GetAnnotations()
	if a != nil {
		var err error
		originalHash, err = base64.StdEncoding.DecodeString(a[CredentialHash])
		if err != nil {
			log.Error(err, "Failed to decode credential hash "+secret.Namespace+"/"+secret.Name)
			return ctrl.Result{}, err
		}
	}

	// We need to extract the specific secret.Data
	secretData, err := extractImportantData(secret)
	if err != nil {
		log.Error(err, "Failed to extract secret.Data[metadata] from "+secret.Namespace+"/"+secret.Name)
		return ctrl.Result{}, err
	}

	log.V(0).Info("Calculate the current hash for provider credential secret " + secret.Namespace + "/" + secret.Name)
	secretBytes, err := json.Marshal(secretData)
	if err != nil {
		log.Error(err, "Failed to marshal secret data json for SHA256 hashing")
		return ctrl.Result{}, err
	}

	// Generate a hash from the Provider secret Data pairs
	currentHash, err := generateHash(secretBytes)
	if err != nil {
		log.Error(err, "Failed to hash secret data")
		return ctrl.Result{}, err
	}

	log.V(0).Info("ORIGINAL Provider hash: " + base64.StdEncoding.EncodeToString([]byte(originalHash)))
	log.V(0).Info("NEW Provider hash: " + base64.StdEncoding.EncodeToString([]byte(currentHash)))

	// If no hash is found, store the currentHash (this is for NEW or MIGRATED Provider Secrets)
	if originalHash == nil {

		log.V(0).Info("Store initial hash for the Provider secret")

		// If the originalHash and currentHash don't match, an update has occured
	} else if bytes.Compare(originalHash, currentHash) != 0 {

		log.V(0).Info("Provider secret data has changed, reconcile ALL copies")

		// Retreives all copied secrets that have labels pointing to this Provider
		secrets := &corev1.SecretList{}
		err = r.APIReader.List(
			ctx,
			secrets,
			client.MatchingLabels{copiedFromNamespaceLabel: req.Namespace, copiedFromNameLabel: req.Name})

		// Check if we found any copies
		secretCount := len(secrets.Items)
		if err != nil || secretCount == 0 {
			log.V(0).Info("Did not find any copied secrets")
			return ctrl.Result{}, nil
		}

		log.V(0).Info("Found " + strconv.Itoa(secretCount) + " copies")

		// Loop through all retreived copies

		for i := range secrets.Items {

			childSecret := secrets.Items[i]

			log.V(0).Info("Child secret:" + childSecret.Namespace + "/" + childSecret.Name)

			// The copiedFrom* labels are self-asserted by the child and the
			// hash gate below only proves knowledge of the prior plaintext,
			// so neither establishes that the child lives in a namespace
			// the hub actually controls. Require that the child's
			// namespace be a Joined ManagedCluster before propagating.
			if !isJoinedManagedClusterNamespace(ctx, r.APIReader, childSecret.Namespace) {
				msg := "namespace " + childSecret.Namespace +
					" is not a Joined ManagedCluster; refusing to propagate credentials from " +
					secret.Namespace + "/" + secret.Name + " into " +
					childSecret.Namespace + "/" + childSecret.Name
				log.V(0).Info("|--X Skipping secret " + childSecret.Namespace + "/" + childSecret.Name + ": " + msg)
				if r.Recorder != nil {
					r.Recorder.Event(&childSecret, corev1.EventTypeWarning, UnauthorizedCredentialCopyEventReason, msg)
				}
				continue
			}

			secretBytes, err := json.Marshal(childSecret.Data)
			if err != nil {
				log.Error(err, "Failed to marshal secret data for hashing")
				continue
			}

			/* Hash the secret.data to rule out an injection attack. The copied secret.data
			   should hash to the same value as the Provider secret's originalHash.
			   If they differ, someone may have attempted to falsify this copied secret so
			   we will log a warning and SKIP updating this secret with the new credentials.
			*/
			childHash, err := generateHash(secretBytes)
			if err != nil {
				log.Error(err, "Failed to hash secret data")
				return ctrl.Result{}, err
			}

			log.V(0).Info("Child hash: " + base64.StdEncoding.EncodeToString([]byte(childHash)))

			// If both hashes match, the copied secret is from the Provider
			if bytes.Compare(originalHash, childHash) == 0 {
				log.V(0).Info("Child secret hash matches, update the child secret")

				childSecret.Data = secretData
				if err := r.Client.Update(ctx, &childSecret); err != nil {
					log.Error(err, "|--X Failed to update child secret: "+childSecret.Namespace+"/"+childSecret.Name)
				}
				log.V(0).Info("|--> Updated secret: " + childSecret.Namespace + "/" + childSecret.Name)

				// The hashes don't match, so this copied secret can NOT be trusted
			} else {
				log.V(0).Info("|--X Did not update secret: " +
					childSecret.Namespace + "/" +
					childSecret.Name +
					", hash did not match")

				klog.Infof("originalHash: %v", base64.StdEncoding.EncodeToString([]byte(originalHash)))
				klog.Infof("childHash: %v", base64.StdEncoding.EncodeToString([]byte(childHash)))
			}
		}
	} else {
		log.V(0).Info("Provider secret data has not changed")

		return ctrl.Result{}, nil
	}

	/* When we finish processing all copied secrets, update the Provider secret with the currentHash

	   This also saves us in a failure. For example, if we only got half way through processing copied secrets
	   and the pod is goes down, when the pod restarts, it will detect that the Provider originalHash
	   does not match the currentHash and will start to process all copied secrets. As it looks at the first
	   half of the copied secrets (those already updated), it will just throw warnings that the hashes don't
	   match. Once it gets to the copied secrets that were not processed, they will be updated as usual.
	   When all the copied secrets are updated, the currentHash is written to the originalHash and
	   the processing is complete.
	*/

	currentCredHash := base64.StdEncoding.EncodeToString([]byte(currentHash))
	patch := &map[string]interface{}{
		"metadata": map[string]interface{}{
			"annotations": map[string]string{
				CredentialHash: currentCredHash,
			},
		},
	}
	patchBytes, err := json.Marshal(patch)

	err = r.Patch(context.Background(), &corev1.Secret{
		ObjectMeta: v1.ObjectMeta{
			Name:      secret.Name,
			Namespace: secret.Namespace,
		},
	}, client.RawPatch(types.StrategicMergePatchType, patchBytes))

	if err != nil {
		log.Error(err, "Failed to patch the Provider secret annotation with the new hash")
	}
	log.V(0).Info("Updated Provider secret hash")

	return ctrl.Result{}, nil
}

func (r *ProviderCredentialSecretReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Secret{}).WithEventFilter(predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			switch e.Object.GetLabels()[ProviderTypeLabel] {
			case "ans", "aws", "gcp", "vmw", "azr", "ost", "redhatvirtualization": //, "bm"
				return true
			}
			// Add the hash check here??

			return false
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			switch e.ObjectNew.GetLabels()[ProviderTypeLabel] {
			case "ans", "aws", "gcp", "vmw", "azr", "ost", "redhatvirtualization": //, "bm"
				return true
			}
			return false
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return false
		},
	}).WithOptions(controller.Options{
		MaxConcurrentReconciles: 1, // This is the default
	}).Complete(r)
}

func extractImportantData(credentialSecret corev1.Secret) (map[string][]byte, error) {
	returnData := map[string][]byte{}

	var err error

	// NOTE: The hash is dependent on the KEY order.  Keys are sorted alphabetically when
	//       kubernetes encodes from secret.stringData to secret.Data
	credType := credentialSecret.ObjectMeta.Labels[ProviderTypeLabel]

	switch credType {
	case "ans":
		returnData = credentialSecret.Data

	case "aws":
		returnData["aws_access_key_id"] = credentialSecret.Data["aws_access_key_id"]
		returnData["aws_secret_access_key"] = credentialSecret.Data["aws_secret_access_key"]

	case "azr":
		returnData["osServicePrincipal.json"] = credentialSecret.Data["osServicePrincipal.json"]

	case "gcp":
		returnData["osServiceAccount.json"] = credentialSecret.Data["osServiceAccount.json"]

	case "vmw":
		returnData["password"] = credentialSecret.Data["password"]
		returnData["username"] = credentialSecret.Data["username"]

	case "ost":
		returnData["cloud"] = credentialSecret.Data["cloud"]
		returnData["clouds.yaml"] = credentialSecret.Data["clouds.yaml"]

	case "redhatvirtualization":
		returnData["ovirt-config.yaml"] = []byte(fmt.Sprintf(
			rhvConfigTemplate,
			credentialSecret.Data["ovirt_url"],
			credentialSecret.Data["ovirt_username"],
			credentialSecret.Data["ovirt_password"],
			indent(2, credentialSecret.Data["ovirt_ca_bundle"]),
		))

	default:
		err = errors.New("Label:" + ProviderTypeLabel + " is not supported for value: " + credType)
	}

	return returnData, err
}

func indent(indention int, v []byte) string {
	newline := "\n" + strings.Repeat(" ", indention)
	return strings.Replace(string(v), "\n", newline, -1)
}
