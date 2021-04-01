// Copyright Contributors to the Open Cluster Management project.

package controllers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"

	"github.com/go-logr/logr"
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
const providerLabel = "cluster.open-cluster-management.io/provider"
const cloneFromLabelNamespace = "cluster.open-cluster-management.io/copiedFromNamespace"
const cloneFromLabelName = "cluster.open-cluster-management.io/copiedFromSecretName"

var hash = sha256.New()

// ProviderCredentialSecretReconciler reconciles a Provider secret
type ProviderCredentialSecretReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

func generateHash(valueBytes []byte) []byte {

	hash.Reset()
	hash.Write(valueBytes)

	return hash.Sum(nil)
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
	originalHash := secret.Data[CredHash]

	// This is the new current secret.Data
	secretData := secret.Data
	delete(secretData, CredHash)

	log.V(1).Info("Calculate the current hash for provider credential secret " + secret.Namespace + "/" + secret.Name)
	secretBytes, err := json.Marshal(secretData)
	if err != nil {
		log.Error(err, "Failed to marshal secret data josn for SHA256 hasing")
		return ctrl.Result{}, err
	}

	// Generate a hash from the Provider secret Data pairs
	currentHash := generateHash(secretBytes)

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
		err = r.List(ctx, secrets, client.MatchingLabels{cloneFromLabelNamespace: req.Namespace, cloneFromLabelName: req.Name})

		// Check if we found any copies
		secretCount := len(secrets.Items)
		if err != nil || secretCount == 0 {
			log.V(1).Info("Did not find any copied secrets")
		}

		log.V(1).Info("Found " + strconv.Itoa(secretCount) + " copies")

		// Loop through all retreived copies
		for _, childSecret := range secrets.Items {

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
			childHash := generateHash(secretBytes)
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
				log.V(0).Info("|--X Did not update secret: " + childSecret.Namespace + "/" + childSecret.Name + ", hash did not match")
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

	patch := &map[string]interface{}{
		"data": map[string]([]byte){
			CredHash: currentHash,
		},
	}
	patchBytes, _ := json.Marshal(patch)

	r.Patch(context.Background(), &corev1.Secret{
		ObjectMeta: v1.ObjectMeta{
			Name:      secret.Name,
			Namespace: secret.Namespace,
		},
	}, client.RawPatch(types.StrategicMergePatchType, patchBytes))

	if err != nil {
		log.Error(err, "Failed to patch the Provider secret.data with the new hash")
	}
	log.V(1).Info("Updated Provider secret hash")

	return ctrl.Result{}, nil
}

func (r *ProviderCredentialSecretReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Secret{}).WithEventFilter(predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			switch e.Object.GetLabels()[providerLabel] {
			case "ans": //"aws", "gcp", "vmw", "azr", "bm"
				return true
			}
			// Add the hash check here??

			return false
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			switch e.ObjectNew.GetLabels()[providerLabel] {
			case "ans": // "aws", "gcp", "vmw", "azr", "bm"
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
