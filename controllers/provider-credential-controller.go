// Copyright Contributors to the Open Cluster Management project.

package controllers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/go-logr/logr"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const CredHash = "credential-hash"
const CredentialHash = "credentialHash"
const providerLabel = "cluster.open-cluster-management.io/type"
const copiedFromNamespaceLabel = "cluster.open-cluster-management.io/copiedFromNamespace"
const copiedFromNameLabel = "cluster.open-cluster-management.io/copiedFromSecretName"
const CredentialLabel = "cluster.open-cluster-management.io/credentials"

var hash = sha256.New()

// ProviderCredentialSecretReconciler reconciles a Provider secret
type ProviderCredentialSecretReconciler struct {
	client.Client
	APIReader client.Reader
	Log       logr.Logger
	Scheme    *runtime.Scheme
}

func generateHash(valueBytes []byte) ([]byte, error) {

	hash.Reset()
	_, err := hash.Write(valueBytes)

	return hash.Sum(nil), err
}

func (r *ProviderCredentialSecretReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {

	log := r.Log.WithValues("ProviderCredentialSecretReconciler", req.NamespacedName)

	var secret corev1.Secret
	if err := r.Get(ctx, req.NamespacedName, &secret); err != nil {
		log.V(1).Info("Resource deleted")
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

	//We need to extract the specific secret.Data
	secretData, err := extractImportantData(secret)
	if err != nil {
		log.Error(err, "Failed to extract secret.Data[metadata] from "+secret.Namespace+"/"+secret.Name)
		return ctrl.Result{}, err
	}

	log.V(1).Info("Calculate the current hash for provider credential secret " + secret.Namespace + "/" + secret.Name)
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

	log.V(1).Info("ORIGINAL Provider hash: " + hex.EncodeToString(originalHash))
	log.V(1).Info("NEW Provider hash: " + hex.EncodeToString(currentHash))

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
			log.V(1).Info("Did not find any copied secrets")
			return ctrl.Result{}, nil
		}

		log.V(1).Info("Found " + strconv.Itoa(secretCount) + " copies")

		// Loop through all retreived copies

		for i := range secrets.Items {

			childSecret := secrets.Items[i]

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

			log.V(1).Info("Child hash: " + hex.EncodeToString(childHash))

			// If both hashes match, the copied secret is from the Provider
			if bytes.Compare(originalHash, childHash) == 0 {

				log.V(1).Info("Child secret hash matches, update the child secret")

				childSecret.Data = secretData
				err = r.Client.Update(ctx, &childSecret)
				if err != nil {
					log.Error(err, "|--X Failed to update child secret: "+childSecret.Namespace+"/"+childSecret.Name)
				}
				log.V(0).Info("|--> Updated secret: " + childSecret.Namespace + "/" + childSecret.Name)

				// The hashes don't match, so this copied secret can NOT be trusted
			} else {
				log.V(0).Info("|--X Did not update secret: " +
					childSecret.Namespace + "/" +
					childSecret.Name +
					", hash did not match")
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
	log.V(1).Info("Updated Provider secret hash")

	return ctrl.Result{}, nil
}

func (r *ProviderCredentialSecretReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Secret{}).WithEventFilter(predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			switch e.Object.GetLabels()[providerLabel] {
			case "ans", "aws", "gcp", "vmw", "azr", "ost": //, "bm"
				return true
			}
			// Add the hash check here??

			return false
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			switch e.ObjectNew.GetLabels()[providerLabel] {
			case "ans", "aws", "gcp", "vmw", "azr", "ost": //, "bm"
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

	providerMetadata, err := extractCredentialFromMetadata(credentialSecret.Data)

	//_, err := extractCredentialFromMetadata(credentialSecret.Data)

	// NOTE: The hash is dependent on the KEY order.  Keys are sorted alphabetically when
	//       kubernetes encodes from secret.stringData to secret.Data
	credType := credentialSecret.ObjectMeta.Labels[providerLabel]
	switch credType {

	case "ans":
		returnData = credentialSecret.Data
		delete(returnData, CredHash)

		err = nil

	case "aws":

		returnData["aws_access_key_id"] = credentialSecret.Data["aws_access_key_id"]
		returnData["aws_secret_access_key"] = credentialSecret.Data["aws_secret_access_key"]

	case "azr":

		// Build the osServicePrincipal json string as a byte slice
		// returnData["osServicePrincipal.json"] = []byte("{\"clientId\": \"" + string(providerMetadata["clientId"]) +
		// 	"\", \"clientSecret\": \"" + string(providerMetadata["clientSecret"]) + "\", \"tenantId\": \"" +
		// 	string(providerMetadata["tenantId"]) + "\", \"subscriptionId\": \"" +
		// 	string(providerMetadata["subscriptionId"]) + "\"}")
		returnData["osServicePrincipal.json"] = credentialSecret.Data["osServicePrincipal.json"]

	case "gcp":

		returnData["osServiceAccount.json"] = credentialSecret.Data["osServicePrincipal.json"]

	case "vmw":

		returnData["password"] = credentialSecret.Data["password"]
		returnData["username"] = credentialSecret.Data["username"]

	case "ost":

		returnData["cloud"] = credentialSecret.Data["cloud"]
		returnData["clouds.yaml"] = credentialSecret.Data["clouds.yaml"]

	default:
		err = errors.New("Label:" + providerLabel + " is not supported for value: " + credType)
	}

	return returnData, err
}

func extractCredentialFromMetadata(secretData map[string][]byte) (map[string]string, error) {
	if bytes.Compare(secretData["metadata"], []byte{}) == 0 {
		return nil, errors.New("Did not find any credential information with key: metadata")
	}
	providerMetadata := map[string]string{}

	err := yaml.Unmarshal(secretData["metadata"], &providerMetadata)

	return providerMetadata, err
}
