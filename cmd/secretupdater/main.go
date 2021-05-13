// Copyright Contributors to the Open Cluster Management project.

package main

import (
	"context"
	"fmt"
	"os"

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
	updateSecretLabels(kubeclient)
}
func newK8s(conf *rest.Config) (client.Client, error) {
	kubeClient, err := client.New(conf, client.Options{})
	if err != nil {
		klog.V(0).Info("Failed to initialize a client connection to the cluster", "error", err.Error())
		return nil, fmt.Errorf("Failed to initialize a client connection to the cluster")
	}
	return kubeClient, nil
}
func updateSecretLabels(c client.Client) {
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
			err = c.Update(context.Background(), &secret)

			if err != nil {
				klog.Error(err, "Failed to patch the Provider secret label")
				panic(err)
			} else {
				klog.V(0).Info("Updated secret with new label: ", secret.Name)
			}
		}
	}

	klog.V(0).Info("Done!")
}
