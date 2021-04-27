// Copyright Contributors to the Open Cluster Management project.

package controllers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap/zapcore"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	clientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

const CPSName = "my-cloud-provider-secret"
const CPSNamespace = "providers"
const ClusterNamespace1 = "cluster1"
const ClusterNamespace2 = "cluster2"
const TOKEN = "token"
const HOST = "host"

var s = scheme.Scheme

func init() {
	corev1.SchemeBuilder.AddToScheme(s)
}

func getCPSecret() corev1.Secret {
	return corev1.Secret{
		ObjectMeta: v1.ObjectMeta{
			Namespace: CPSNamespace,
			Name:      CPSName,
		},
		Data: map[string][]byte{
			HOST:  []byte("https://hello.io"),
			TOKEN: []byte("ABDCDEFJD333299943mmienw"),
		},
	}
}
func GetProviderCredentialSecretReconciler() *ProviderCredentialSecretReconciler {

	ctrl.SetLogger(zap.New(zap.UseDevMode(true), zap.Level(zapcore.InfoLevel)))

	return &ProviderCredentialSecretReconciler{
		Client: clientfake.NewFakeClientWithScheme(s),
		Log:    ctrl.Log.WithName("controllers").WithName("ProviderCredentialSecretReconciler"),
		Scheme: s,
	}
}

func getRequest() ctrl.Request {
	return ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: CPSNamespace,
			Name:      CPSName,
		},
	}
}

// Since we should not reconcile on delete we should not get to this
func TestReconcileNoSecret(t *testing.T) {

	cpsr := GetProviderCredentialSecretReconciler()

	_, err := cpsr.Reconcile(context.Background(), getRequest())

	assert.NotNil(t, err, "Not nil, when Provider secret does not exist")
	t.Logf("Error: %v", err)
}

func TestReconcileNewCPSecret(t *testing.T) {

	// Missing "bm"
	for _, providerName := range []string{"ans", "aws", "gcp", "vmw", "ost", "azr"} {

		cps := getCPSecret()
		cps.ObjectMeta.Labels = map[string]string{
			providerLabel: providerName,
		}
		cps.Data["metadata"] = []byte("fakeKey: fakeValue\n")

		cpsr := GetProviderCredentialSecretReconciler()
		cpsr.Client = clientfake.NewFakeClient(&cps)

		// Test the function
		_, err := cpsr.Reconcile(context.Background(), getRequest())

		assert.Nil(t, err, "Nil, when Provider secret found, and hash is set")

		// Check that the credential-hash was set
		cpsr.Get(context.Background(), getRequest().NamespacedName, &cps)

		assert.NotNil(t, cps.Data[CredHash], CredHash+" should not be nil")
		t.Logf("Hash: %v", cps.Data[CredHash])
	}
}

func TestReconcileInvalidProviderLabel(t *testing.T) {

	// Missing "bm"
	for _, providerName := range []string{"invalid"} {

		cps := getCPSecret()
		cps.ObjectMeta.Labels = map[string]string{
			providerLabel: providerName,
		}
		cps.Data["metadata"] = []byte("fakeKey: fakeValue\n")

		cpsr := GetProviderCredentialSecretReconciler()
		cpsr.Client = clientfake.NewFakeClient(&cps)

		// Test the function
		_, err := cpsr.Reconcile(context.Background(), getRequest())

		assert.NotNil(t, err, "Not nil, when Provider secret found, but label is invalid")
		t.Logf("error mesage: %v", err)
	}
}

func TestReconcileNoCPSecretChange(t *testing.T) {

	cps := getCPSecret()
	cps.ObjectMeta.Labels = map[string]string{
		providerLabel: "ans",
	}

	cpsr := GetProviderCredentialSecretReconciler()
	cpsr.Client = clientfake.NewFakeClient(&cps)

	// Test the function try #1 (Initializes credential-hash)
	t.Log("Try 1")
	_, err := cpsr.Reconcile(context.Background(), getRequest())

	assert.Nil(t, err, "Nil, when Cloud Provider secret found, and hash is set")

	// Check that the credential-hash was set
	cpsr.Get(context.Background(), getRequest().NamespacedName, &cps)

	try1Hash := cps.Data[CredHash]
	assert.NotNil(t, try1Hash, CredHash+" should not be nil")
	t.Logf("Hash: %v", try1Hash)

	// Test the function try #2 (Initializes credential-hash)
	t.Log("Try 2")
	_, err = cpsr.Reconcile(context.Background(), getRequest())

	assert.Nil(t, err, "Nil, when Cloud Provider secret found")

	// Check that the credential-hash was set
	cpsr.Get(context.Background(), getRequest().NamespacedName, &cps)

	assert.NotNil(t, cps.Data[CredHash], CredHash+" should not be nil")
	t.Logf("Hash: %v", cps.Data[CredHash])

	//Compare Try #1 and Try #2, the credential-hash should be the same
	assert.Equal(t, try1Hash, cps.Data[CredHash], "Hashes are equal")
}

func TestReconcileChildSecrets(t *testing.T) {

	cps := getCPSecret()
	cps.ObjectMeta.Labels = map[string]string{
		providerLabel: "ans",
	}

	cpsr := GetProviderCredentialSecretReconciler()
	cpsr.Client = clientfake.NewFakeClient(&cps)

	// Try #1 initializes the credential-hash
	_, err := cpsr.Reconcile(context.Background(), getRequest())

	assert.Nil(t, err, "Nil, when Cloud Provider secret found, and hash is set")

	// Check that the credential-hash was set
	cpsr.Get(context.Background(), getRequest().NamespacedName, &cps)
	cps.Data[TOKEN] = []byte("something-new")
	cpsr.Update(context.Background(), &cps)

	copy1 := getCPSecret()
	copy2 := getCPSecret()

	labels := map[string]string{
		cloneFromLabelNamespace: CPSNamespace,
		cloneFromLabelName:      CPSName,
	}

	copy1.ObjectMeta.Labels = labels
	copy1.ObjectMeta.Name = CPSName + "1"
	copy1.ObjectMeta.Namespace = CPSName + "1"
	copy2.ObjectMeta.Labels = labels
	copy2.ObjectMeta.Name = CPSName + "2"
	copy2.ObjectMeta.Namespace = CPSName + "2"

	cpsr.Create(context.Background(), &copy1)
	cpsr.Create(context.Background(), &copy2)

	// Try #2 Update copied secrets
	_, err = cpsr.Reconcile(context.Background(), getRequest())

	assert.Nil(t, err, "Nil, when Cloud Provider secret found, and hash is set")

}

func TestReconcileChangeWithNoCopiedSecrets(t *testing.T) {

	cps := getCPSecret()
	cps.ObjectMeta.Labels = map[string]string{
		providerLabel: "ans",
	}

	cpsr := GetProviderCredentialSecretReconciler()
	cpsr.Client = clientfake.NewFakeClient(&cps)

	// Try #1 initializes the credential-hash
	_, err := cpsr.Reconcile(context.Background(), getRequest())

	assert.Nil(t, err, "Nil, when Cloud Provider secret found, and hash is set")

	// Check that the credential-hash was set
	cpsr.Get(context.Background(), getRequest().NamespacedName, &cps)
	cps.Data[TOKEN] = []byte("something-new")
	cpsr.Update(context.Background(), &cps)

	// Try #2 Update copied secrets
	_, err = cpsr.Reconcile(context.Background(), getRequest())

	assert.Nil(t, err, "Nil, when Cloud Provider secret found, and hash is set")

}

func TestReconcileChildSecretsInjectionAttack(t *testing.T) {

	cps := getCPSecret()
	cps.ObjectMeta.Labels = map[string]string{
		providerLabel: "ans",
	}

	cpsr := GetProviderCredentialSecretReconciler()
	cpsr.Client = clientfake.NewFakeClient(&cps)

	// Try #1 initializes the credential-hash
	_, err := cpsr.Reconcile(context.Background(), getRequest())

	assert.Nil(t, err, "Nil, when Cloud Provider secret found, and hash is set")

	// Check that the credential-hash was set
	cpsr.Get(context.Background(), getRequest().NamespacedName, &cps)
	cps.Data[TOKEN] = []byte("something-new")
	cpsr.Update(context.Background(), &cps)

	copy1 := getCPSecret()
	copy2 := getCPSecret()

	labels := map[string]string{
		cloneFromLabelNamespace: CPSNamespace,
		cloneFromLabelName:      CPSName,
	}

	copy1.ObjectMeta.Labels = labels
	copy1.ObjectMeta.Name = CPSName + "1"
	copy1.ObjectMeta.Namespace = CPSName + "1"
	copy1.Data[TOKEN] = []byte("my-injected-token")
	copy2.ObjectMeta.Labels = labels
	copy2.ObjectMeta.Name = CPSName + "2"
	copy2.ObjectMeta.Namespace = CPSName + "2"
	copy2.Data[HOST] = []byte("my injected host")

	cpsr.Create(context.Background(), &copy1)
	cpsr.Create(context.Background(), &copy2)

	// Try #2 Update copied secrets
	_, err = cpsr.Reconcile(context.Background(), getRequest())

	assert.Nil(t, err, "Nil, when Cloud Provider secret found, and hash is set")

	// Check the token on copy1 was not modified
	result := corev1.Secret{}
	err = cpsr.Get(context.Background(), types.NamespacedName{
		Namespace: copy1.Namespace,
		Name:      copy1.Name,
	}, &result)

	assert.Equal(t, result.Data[TOKEN], copy1.Data[TOKEN], "Token should not have changed")

	// Check the host on copy2 was not modified
	result = corev1.Secret{}
	err = cpsr.Get(context.Background(), types.NamespacedName{
		Namespace: copy2.Namespace,
		Name:      copy2.Name,
	}, &result)

	assert.Equal(t, result.Data[HOST], copy2.Data[HOST], "Token should not have changed")

	// Check that the Cloud Provider secret's token is not the original
	result = corev1.Secret{}
	err = cpsr.Get(context.Background(), types.NamespacedName{
		Namespace: cps.Namespace,
		Name:      cps.Name,
	}, &result)

	assert.NotEqual(t, result.Data[TOKEN], []byte("ABDCDEFJD333299943mmienw"), "Token should not have changed")

	// Check that it is the secondary value
	assert.Equal(t, result.Data[TOKEN], []byte("something-new"), "Token should not have changed")
}
