// Copyright Contributors to the Open Cluster Management project.

package providercredential

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"

	"github.com/open-cluster-management/library-go/pkg/config"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func init() {
	corev1.SchemeBuilder.AddToScheme(s)
}

func getChildSecret(secretName string) corev1.Secret {
	copiedSecret := getCPSecret()
	copiedSecret.ObjectMeta.Labels = map[string]string{
		copiedFromNamespaceLabel: CPSNamespace,
		copiedFromNameLabel:      CPSName,
	}
	copiedSecret.Namespace = "default"
	copiedSecret.Name = secretName
	return copiedSecret
}

func skipShort(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode.")
	}
}

const SecretCount = 3000

// Since we should not reconcile on delete we should not get to this
func TestInitializeSecrets(t *testing.T) {

	skipShort(t)

	cfg, _ := config.LoadConfig("", "", "")
	c, _ := client.New(cfg, client.Options{})

	providerSecret := getCPSecret()

	providerSecret.ObjectMeta.Labels = map[string]string{
		ProviderTypeLabel: "ans",
	}
	t.Log("Create Provider Credential secret")
	err := c.Create(context.Background(), &providerSecret)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < SecretCount; i++ {

		t.Logf("Create copied secret: secret%v", i)
		copiedSecret := getChildSecret("secret" + strconv.Itoa(i))
		err := c.Create(context.Background(), &copiedSecret)
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestCleanUpSecrets(t *testing.T) {

	skipShort(t)

	cfg, _ := config.LoadConfig("", "", "")
	c, _ := client.New(cfg, client.Options{})

	providerSecret := getCPSecret()
	providerSecret.ObjectMeta.Labels = map[string]string{
		ProviderTypeLabel: "ans",
	}

	err := c.Delete(context.Background(), &providerSecret)
	if err != nil {
		t.Fatal(err)
	}

	copiedSecret := getCPSecret()
	copiedSecret.ObjectMeta.Labels = map[string]string{
		copiedFromNamespaceLabel: providerSecret.Namespace,
		copiedFromNameLabel:      providerSecret.Name,
	}
	copiedSecret.Namespace = "default"

	for i := 0; i < SecretCount; i++ {

		copiedSecret.Name = "secret" + strconv.Itoa(i)
		t.Logf("Deleted secret: secret%v", i)
		err := c.Delete(context.Background(), &copiedSecret)
		if err != nil {
			t.Fatal(err)
		}
	}
}

func updateProviderSecret(t *testing.T, value string) {

	skipShort(t)

	cfg, _ := config.LoadConfig("", "", "")
	c, _ := client.New(cfg, client.Options{})

	patch := &map[string]interface{}{
		"data": map[string]([]byte){
			"token": []byte(value),
		},
	}
	patchBytes, _ := json.Marshal(patch)

	err := c.Patch(context.Background(), &corev1.Secret{
		ObjectMeta: v1.ObjectMeta{
			Name:      CPSName,
			Namespace: CPSNamespace,
		},
	}, client.RawPatch(types.StrategicMergePatchType, patchBytes))
	if err != nil {
		t.Fatal(err)
	}
}

func TestUpdateProviderSecret(t *testing.T) {
	updateProviderSecret(t, "something-new")
}

func TestUpdateProviderSecretTest2(t *testing.T) {
	updateProviderSecret(t, "something-old")
}
