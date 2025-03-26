// Copyright Contributors to the Open Cluster Management project.

package oldproviderconnection

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	"github.com/stolostron/provider-credential-controller/controllers/providercredential"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const CloudConnectionLabel = "cluster.open-cluster-management.io/cloudconnection"
const ProviderLabel = "cluster.open-cluster-management.io/provider"

var mapYamlKeys = map[string]string{
	"awsAccessKeyID":       "aws_access_key_id",
	"awsSecretAccessKeyID": "aws_secret_access_key",
	"sshPrivatekey":        "ssh-privatekey",
	"sshPublickey":         "ssh-publickey",
	"gcServiceAccountKey":  "osServiceAccount.json",
	"gcProjectID":          "projectID",
	"openstackCloudsYaml":  "clouds.yaml",
	"openstackCloud":       "cloud",
	"vcenter":              "vCenter",
	"vmClusterName":        "cluster",
	"datastore":            "defaultDatastore",
}

var hash = sha256.New()

// OldProviderConnectionReconciler reconciles a Old Provider secret
type OldProviderConnectionReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

func (r *OldProviderConnectionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {

	log := r.Log.WithValues("OldProviderConnectionReconciler", req.NamespacedName)

	var secret corev1.Secret
	if err := r.Get(ctx, req.NamespacedName, &secret); err != nil {
		log.V(1).Info("Resource deleted")
		return ctrl.Result{}, err
	}

	log.V(1).Info("Reconcile secret")

	if err := updateSecret(r.Client, secret); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func updateSecret(c client.Client, secret corev1.Secret) error {
	newLabels := make(map[string]string)
	labels := secret.GetLabels()
	if labels != nil {
		for key, val := range labels {
			if key == ProviderLabel {
				newLabels[providercredential.ProviderTypeLabel] = val
			}
			if key != CloudConnectionLabel && key != ProviderLabel {
				newLabels[key] = val
			}

		}
	}
	newLabels[providercredential.CredentialLabel] = ""

	secret.ObjectMeta.Labels = newLabels
	providerMetadata, err := extractSecretMetadata(secret.Data)
	if err != nil {
		klog.Error(err, "\tsecret: ", secret.Name)
		return err
	}

	credType := labels[ProviderLabel]
	switch credType {
	case "azr":
		osServicePrincipal := `{"clientId": "` + fmt.Sprintf("%v", providerMetadata["clientId"]) + `", "clientSecret": "` + fmt.Sprintf("%v", providerMetadata["clientSecret"]) + `", "tenantId": "` + fmt.Sprintf("%v", providerMetadata["tenantId"]) + `", "subscriptionId": "` + fmt.Sprintf("%v", providerMetadata["subscriptionId"]) + `"}`
		secret.Data["osServicePrincipal.json"] = []byte(osServicePrincipal)
		delete(providerMetadata, "clientId")
		delete(providerMetadata, "clientSecret")
		delete(providerMetadata, "tenantId")
		delete(providerMetadata, "subscriptionId")
	}

	for key, meta := range providerMetadata {
		var b []byte
		if key == "sshKnownHosts" {
			var sshKnownhost string
			for _, host := range meta.([]interface{}) {
				sshKnownhost = sshKnownhost + host.(string) + "\n"
			}
			sshKnownhost = strings.TrimSuffix(sshKnownhost, "\n")
			b = []byte(sshKnownhost)
		} else {
			b = []byte(fmt.Sprintf("%v", meta.(interface{})))
		}
		if hiveKey, ok := mapYamlKeys[key]; ok {
			secret.Data[hiveKey] = b
		} else {
			secret.Data[key] = b
		}
	}

	delete(secret.Data, "metadata")
	err = c.Update(context.Background(), &secret)

	if err != nil {
		klog.Error(err, "Failed to patch the Provider secret label")
		return err
	} else {
		klog.V(0).Info("Updated secret with new label and yaml keys: ", secret.Name)
	}
	return nil
}

func (r *OldProviderConnectionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Secret{}).WithEventFilter(predicate.Funcs{
		DeleteFunc: func(e event.DeleteEvent) bool {
			return false
		},
	}).WithOptions(controller.Options{
		MaxConcurrentReconciles: 1, // This is the default
	}).Complete(r)
}

func extractSecretMetadata(secretData map[string][]byte) (map[string]interface{}, error) {
	if bytes.Compare(secretData["metadata"], []byte{}) == 0 {
		return nil, errors.New("Did not find any credential information with key: metadata")
	}
	providerMetadata := map[string]interface{}{}

	err := yaml.Unmarshal(secretData["metadata"], &providerMetadata)
	if err != nil {
		klog.Error(err)
		return nil, errors.New("Failed to unmarshal the Provider secret metadata")
	}

	return providerMetadata, err
}
