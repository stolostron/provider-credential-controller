// Copyright Contributors to the Open Cluster Management project.

package providercredential

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
const userValue = "https://hello.io"
const tokenValue = "ABDCDEFJD333299943mmienw"

var s = scheme.Scheme

func init() {
	corev1.SchemeBuilder.AddToScheme(s)
}

func getCPSecret() corev1.Secret {
	var dataValue = map[string][]byte{}
	dataValue[HOST] = []byte(userValue)
	dataValue[TOKEN] = []byte(tokenValue)

	return getCPSecretWithKeys(dataValue)
}

func getCopiedSecretForProvider(credentialType string) corev1.Secret {
	var dataValue = map[string][]byte{}
	switch credentialType {
	case "aws":
		dataValue["aws_access_key_id"] = []byte(userValue)
		dataValue["aws_secret_access_key"] = []byte(tokenValue)

	case "azr":
		dataValue["osServicePrincipal.json"] = []byte("{\"clientId\": \"" + tokenValue +
			"ID\", \"clientSecret\": \"" + tokenValue + "SECRET\", \"tenantId\": \"" +
			tokenValue + "TENANT\", \"subscriptionId\": \"" +
			tokenValue + "SUBSCRIPTION\"}")

	case "gcp":
		dataValue["osServiceAccount.json"] = []byte(tokenValue)
	case "vmw":
		dataValue["password"] = []byte(tokenValue)
		dataValue["username"] = []byte(userValue)
	case "ost":
		dataValue["cloud"] = []byte(tokenValue)
		dataValue["clouds.yaml"] = []byte(userValue)
	default:
		panic("Provider did not match " + credentialType)
	}
	return getCPSecretWithKeys(dataValue)

}

func getCPSecretWithKeys(dataValue map[string][]byte) corev1.Secret {

	return corev1.Secret{
		ObjectMeta: v1.ObjectMeta{
			Namespace: CPSNamespace,
			Name:      CPSName,
		},
		Data: dataValue,
	}
}

func getCPSecretMetadata(credentialType string) corev1.Secret {
	var metadataValue string
	switch credentialType {
	case "aws":
		metadataValue = "awsAccessKeyID: " + userValue + "\n" + "awsSecretAccessKeyID: " + tokenValue
	case "azr":
		metadataValue = "clientId: " + tokenValue + "ID\nclientSecret: " + tokenValue + "SECRET\n" +
			"tenantId: " + tokenValue + "TENANT\nsubscriptionId: " + tokenValue + "SUBSCRIPTION"
	case "gcp":
		metadataValue = "gcServiceAccountKey: " + tokenValue
	case "vmw":
		metadataValue = "password: " + tokenValue + "\nusername: " + userValue
	case "ost":
		metadataValue = "openstackCloud: " + tokenValue + "\nopenstackCloudsYaml: " + userValue
	}
	return corev1.Secret{
		ObjectMeta: v1.ObjectMeta{
			Namespace: CPSNamespace,
			Name:      CPSName + "-" + credentialType,
			Labels: map[string]string{
				ProviderTypeLabel: credentialType,
			},
		},
		Data: map[string][]byte{
			"metadata": []byte(metadataValue),
		},
	}
}

func GetProviderCredentialSecretReconciler() *ProviderCredentialSecretReconciler {

	// Log levels: DebugLevel  InfoLevel
	ctrl.SetLogger(zap.New(zap.UseDevMode(true), zap.Level(zapcore.InfoLevel)))

	return &ProviderCredentialSecretReconciler{
		Client:    clientfake.NewFakeClientWithScheme(s),
		APIReader: clientfake.NewFakeClientWithScheme(s),
		Log:       ctrl.Log.WithName("controllers").WithName("ProviderCredentialSecretReconciler"),
		Scheme:    s,
	}
}

func getRequest() ctrl.Request {
	return getRequestWithName(CPSName)
}

func getRequestWithName(rName string) ctrl.Request {
	return ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: CPSNamespace,
			Name:      rName,
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

	for _, providerName := range []string{"ans", "aws", "gcp", "vmw", "ost", "azr"} {

		cps := getCPSecret()
		cps.ObjectMeta.Labels = map[string]string{
			ProviderTypeLabel: providerName,
		}
		cps.Data["metadata"] = []byte("fakeKey: fakeValue\n")

		cpsr := GetProviderCredentialSecretReconciler()
		cpsr.Client = clientfake.NewFakeClient(&cps)

		// Test the function
		_, err := cpsr.Reconcile(context.Background(), getRequest())

		assert.Nil(t, err, "Nil, when Provider secret found, and hash is set")

		// Check that the credential-hash was set
		cpsr.Get(context.Background(), getRequest().NamespacedName, &cps)

		assert.NotNil(t, cps.Annotations[CredentialHash], CredentialHash+" should not be nil")
		t.Logf("Hash: %v", cps.Annotations[CredentialHash])
	}
}

func TestReconcileInvalidProviderLabel(t *testing.T) {

	// Missing "bm"
	for _, providerName := range []string{"invalid"} {

		cps := getCPSecret()
		cps.ObjectMeta.Labels = map[string]string{
			ProviderTypeLabel: providerName,
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
		ProviderTypeLabel: "ans",
	}

	cpsr := GetProviderCredentialSecretReconciler()
	cpsr.Client = clientfake.NewFakeClient(&cps)

	// Test the function try #1 (Initializes credential-hash)
	t.Log("Try 1")
	_, err := cpsr.Reconcile(context.Background(), getRequest())

	assert.Nil(t, err, "Nil, when Cloud Provider secret found, and hash is set")

	// Check that the credential-hash was set
	cpsr.Get(context.Background(), getRequest().NamespacedName, &cps)

	try1Hash := cps.Annotations[CredentialHash]
	assert.NotNil(t, try1Hash, CredentialHash+" should not be nil")
	t.Logf("Hash: %v", try1Hash)

	// Test the function try #2 (Initializes credential-hash)
	t.Log("Try 2")
	_, err = cpsr.Reconcile(context.Background(), getRequest())

	assert.Nil(t, err, "Nil, when Cloud Provider secret found")

	// Check that the credential-hash was set
	cpsr.Get(context.Background(), getRequest().NamespacedName, &cps)

	assert.NotNil(t, cps.Annotations[CredentialHash], CredentialHash+" should not be nil")
	t.Logf("Hash: %v", cps.Annotations[CredentialHash])

	//Compare Try #1 and Try #2, the credential-hash should be the same
	assert.Equal(t, try1Hash, cps.Annotations[CredentialHash], "Hashes are equal")
}

func TestReconcileChildSecrets(t *testing.T) {

	cps := getCPSecret()
	cps.ObjectMeta.Labels = map[string]string{
		ProviderTypeLabel: "ans",
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
		copiedFromNamespaceLabel: CPSNamespace,
		copiedFromNameLabel:      CPSName,
	}

	copy1.ObjectMeta.Labels = labels
	copy1.ObjectMeta.Name = CPSName + "1"
	copy1.ObjectMeta.Namespace = CPSName + "1"
	copy2.ObjectMeta.Labels = labels
	copy2.ObjectMeta.Name = CPSName + "2"
	copy2.ObjectMeta.Namespace = CPSName + "2"

	cpsr.Create(context.Background(), &copy1)
	cpsr.Create(context.Background(), &copy2)

	cpsr.APIReader = clientfake.NewFakeClient(&cps, &copy1, &copy2)

	// Try #2 Update copied secrets
	_, err = cpsr.Reconcile(context.Background(), getRequest())

	assert.Nil(t, err, "Nil, when Cloud Provider secret found, and hash is set")

}

func TestReconcileChildSecretsAllCloudProviders(t *testing.T) {

	for _, provider := range []string{"aws", "gcp", "azr", "vmw", "ost"} {
		t.Logf("Testing credential type: %v", provider)

		cps := getCPSecretMetadata(provider)

		cpsr := GetProviderCredentialSecretReconciler()
		cpsr.Client = clientfake.NewFakeClient(&cps)

		// Try #1 initializes the credential-hash
		_, err := cpsr.Reconcile(context.Background(), getRequestWithName(cps.Name))

		assert.Nil(t, err, "Nil, when Cloud Provider secret found, and hash is set")

		// Check that the credential-hash was set
		cpsr.Get(context.Background(), getRequestWithName(cps.Name).NamespacedName, &cps)

		switch provider {
		case "aws":
			cps.Data["metadata"] = []byte("awsAccessKeyID: NEW_VALUE\n" + "awsSecretAccessKeyID: " + tokenValue)
		case "azr":
			cps.Data["metadata"] = []byte("clientId: NEW_VALUE\nclientSecret: " + tokenValue + "SECRET\n" +
				"tenantId: " + tokenValue + "TENANT\nsubscriptionId: " + tokenValue + "SUBSCRIPTION")
		case "gcp":
			cps.Data["metadata"] = []byte("gcServiceAccountKey: NEW_VALUE")
		case "vmw":
			cps.Data["metadata"] = []byte("password: " + tokenValue + "\nusername: NEW_VALUE")
		case "ost":
			cps.Data["metadata"] = []byte("openstackCloud: " + tokenValue + "\nopenstackCloudsYaml: NEW_VALUE")
		}

		cpsr.Update(context.Background(), &cps)

		copy1 := getCopiedSecretForProvider(provider)
		copy2 := getCopiedSecretForProvider(provider)

		labels := map[string]string{
			copiedFromNamespaceLabel: CPSNamespace,
			copiedFromNameLabel:      cps.Name,
		}

		copy1.ObjectMeta.Labels = labels
		copy1.ObjectMeta.Name = CPSName + "1"
		copy1.ObjectMeta.Namespace = CPSName + "1"
		copy2.ObjectMeta.Labels = labels
		copy2.ObjectMeta.Name = CPSName + "2"
		copy2.ObjectMeta.Namespace = CPSName + "2"

		cpsr.Create(context.Background(), &copy1)
		cpsr.Create(context.Background(), &copy2)

		cpsr.APIReader = clientfake.NewFakeClient(&cps, &copy1, &copy2)

		// Try #2 Update copied secrets
		_, err = cpsr.Reconcile(context.Background(), getRequestWithName(cps.Name))

		assert.Nil(t, err, "Nil, when Cloud Provider secret found, and hash is set")
	}
}

func TestReconcileChangeWithNoCopiedSecrets(t *testing.T) {

	cps := getCPSecret()
	cps.ObjectMeta.Labels = map[string]string{
		ProviderTypeLabel: "ans",
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

	cpsr.APIReader = clientfake.NewFakeClient(&cps)
	// Try #2 Update copied secrets
	_, err = cpsr.Reconcile(context.Background(), getRequest())

	assert.Nil(t, err, "Nil, when Cloud Provider secret found, and hash is set")

}

func TestReconcileChildSecretsInjectionAttack(t *testing.T) {

	cps := getCPSecret()
	cps.ObjectMeta.Labels = map[string]string{
		ProviderTypeLabel: "ans",
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
		copiedFromNamespaceLabel: CPSNamespace,
		copiedFromNameLabel:      CPSName,
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

	cpsr.Client = clientfake.NewFakeClient(&cps, &copy1, &copy2)
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
