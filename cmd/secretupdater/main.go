// Copyright Contributors to the Open Cluster Management project.

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

const CloudConnectionLabel = "cluster.open-cluster-management.io/cloudconnection"
const CredentialLabel = "cluster.open-cluster-management.io/credentials"
const ProviderLabel = "cluster.open-cluster-management.io/provider"
const TypeLabel = "cluster.open-cluster-management.io/type"

var mapYamlKeys = map[string]string{
	"awsAccessKeyID":       "aws_access_key_id",
	"awsSecretAccessKeyID": "aws_secret_access_key",
	"sshPrivatekey":        "ssh-privatekey",
	"sshPublickey":         "ssh-publickey",
	"gcServiceAccountKey":  "osServiceAccount.json",
	"openstackCloudsYaml":  "clouds.yaml",
	"openstackCloud":       "cloud",
}

func main() {
	// Get a config to talk to the apiserver
	cfg, err := config.GetConfig()
	if err != nil {
		klog.Error(err, "unable to get config")
		os.Exit(1)
	}

	kubeclient, err := newK8s(cfg)
	if err != nil {
		klog.Error(err, "")
		os.Exit(1)
	}
	updateSecret(kubeclient)
}
func newK8s(conf *rest.Config) (client.Client, error) {
	kubeClient, err := client.New(conf, client.Options{})
	if err != nil {
		klog.V(0).Info("Failed to initialize a client connection to the cluster", "error", err.Error())
		return nil, fmt.Errorf("Failed to initialize a client connection to the cluster")
	}
	return kubeClient, nil
}
func updateSecret(c client.Client) {
	secrets := &corev1.SecretList{}
	err := c.List(
		context.TODO(),
		secrets,
		client.MatchingLabels{CloudConnectionLabel: ""})

	// Check if we found any copies
	secretCount := len(secrets.Items)
	if err != nil || secretCount == 0 {
		klog.V(0).Info("Did not find secrets with label: ", CloudConnectionLabel)
	} else {
		for _, secret := range secrets.Items {
			newLabels := make(map[string]string)
			labels := secret.GetLabels()
			if labels != nil {
				for key, val := range labels {
					if key == ProviderLabel {
						newLabels[TypeLabel] = val
					}
					if key != CloudConnectionLabel && key != ProviderLabel {
						newLabels[key] = val
					}

				}
			}
			newLabels[CredentialLabel] = ""

			secret.ObjectMeta.Labels = newLabels
			providerMetadata, err := extractSecretMetadata(secret.Data)
			if err != nil {
				klog.Error(err, "\tsecret: ", secret.Name)
				continue
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
				continue
			} else {
				klog.V(0).Info("Updated secret with new label and yaml keys: ", secret.Name)
			}
		}
	}

	klog.V(0).Info("Done!")
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
