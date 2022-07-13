package oldproviderconnection

import (
	"context"
	"reflect"
	"testing"

	"go.uber.org/zap/zapcore"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/stolostron/provider-credential-controller/controllers/providercredential"
)

func TestReconcile(t *testing.T) {
	ctrl.SetLogger(zap.New(zap.UseDevMode(true), zap.Level(zapcore.DebugLevel)))

	secretNamespace := "secret1"
	secretName := "test-ns"
	azrMetadata := "clientId: a\nclientSecret: b\ntenantId: c\nsubscriptionId: d"
	hostsMetadata := "sshKnownHosts:\n  - a\n  - b"
	mappingMetadata := "awsAccessKeyID: a\nawsSecretAccessKeyID: b\nsshPrivatekey: c\nsshPublickey: d\ngcServiceAccountKey: e\ngcProjectID: f\nopenstackCloudsYaml: g\nopenstackCloud: h\nvcenter: i\nvmClusterName: j\ndatastore: k\nothers: others-values"

	tests := []struct {
		name         string
		secret       *corev1.Secret
		validateFunc func(c client.Client, err error, t *testing.T)
	}{
		{
			name: "secret doesn't exist",
			validateFunc: func(c client.Client, err error, t *testing.T) {
				if err == nil {
					t.Fatalf("error expected")
				}
				if !errors.IsNotFound(err) {
					t.Fatalf("expect not found error, but got %v", err)
				}
			},
		},
		{
			name: "process label",
			secret: newSecret(secretNamespace, secretName, map[string]string{
				ProviderLabel:        "aws",
				CloudConnectionLabel: "abc",
				"other-label":        "other-value",
			}, azrMetadata),
			validateFunc: func(c client.Client, err error, t *testing.T) {
				if err != nil {
					t.Fatalf("unexpected err: %v", err)
				}

				var secret corev1.Secret
				if err := c.Get(context.Background(), types.NamespacedName{
					Namespace: secretNamespace,
					Name:      secretName,
				}, &secret); err != nil {
					t.Fatalf("failed to fetch secret: %v", err)
				}

				// check if labes processed as expected
				expectedLabels := map[string]string{
					providercredential.ProviderTypeLabel: "aws",
					"other-label":                        "other-value",
					providercredential.CredentialLabel:   "",
				}
				if !reflect.DeepEqual(secret.Labels, expectedLabels) {
					t.Fatalf("expected labels %v, but got %v", expectedLabels, secret.Labels)
				}
			},
		},
		{
			name: "handle azr os service principal",
			secret: newSecret(secretNamespace, secretName, map[string]string{
				ProviderLabel: "azr",
			}, azrMetadata),
			validateFunc: func(c client.Client, err error, t *testing.T) {
				if err != nil {
					t.Fatalf("unexpected err: %v", err)
				}

				var secret corev1.Secret
				if err := c.Get(context.Background(), types.NamespacedName{
					Namespace: secretNamespace,
					Name:      secretName,
				}, &secret); err != nil {
					t.Fatalf("failed to fetch secret: %v", err)
				}

				osServicePrincipal := `{"clientId": "a", "clientSecret": "b", "tenantId": "c", "subscriptionId": "d"}`
				if !reflect.DeepEqual(secret.Data["osServicePrincipal.json"], []byte(osServicePrincipal)) {
					t.Fatalf("expected %v, but got %v", osServicePrincipal, string(secret.Data["osServicePrincipal.json"]))
				}
			},
		},
		{
			name:   "process sshKnownHosts",
			secret: newSecret(secretNamespace, secretName, nil, hostsMetadata),
			validateFunc: func(c client.Client, err error, t *testing.T) {
				if err != nil {
					t.Fatalf("unexpected err: %v", err)
				}

				var secret corev1.Secret
				if err := c.Get(context.Background(), types.NamespacedName{
					Namespace: secretNamespace,
					Name:      secretName,
				}, &secret); err != nil {
					t.Fatalf("failed to fetch secret: %v", err)
				}

				expectedData := map[string][]byte{
					"sshKnownHosts": []byte("a\nb"),
				}

				if !reflect.DeepEqual(secret.Data, expectedData) {
					t.Fatalf("expected data %v, but got %v", expectedData, secret.Data)
				}
			},
		},
		{
			name:   "map metadata to key",
			secret: newSecret(secretNamespace, secretName, nil, mappingMetadata),
			validateFunc: func(c client.Client, err error, t *testing.T) {
				if err != nil {
					t.Fatalf("unexpected err: %v", err)
				}

				var secret corev1.Secret
				if err := c.Get(context.Background(), types.NamespacedName{
					Namespace: secretNamespace,
					Name:      secretName,
				}, &secret); err != nil {
					t.Fatalf("failed to fetch secret: %v", err)
				}

				expectedData := map[string][]byte{
					"aws_access_key_id":     []byte("a"),
					"aws_secret_access_key": []byte("b"),
					"ssh-privatekey":        []byte("c"),
					"ssh-publickey":         []byte("d"),
					"osServiceAccount.json": []byte("e"),
					"projectID":             []byte("f"),
					"clouds.yaml":           []byte("g"),
					"cloud":                 []byte("h"),
					"vCenter":               []byte("i"),
					"cluster":               []byte("j"),
					"defaultDatastore":      []byte("k"),
					"others":                []byte("others-values"),
				}

				if !reflect.DeepEqual(secret.Data, expectedData) {
					t.Fatalf("expected data %v, but got %v", expectedData, secret.Data)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a fake client to mock API calls.
			objs := []runtime.Object{}
			if tt.secret != nil {
				objs = append(objs, tt.secret)
			}
			fakeClient := fake.NewFakeClient(objs...)
			s := scheme.Scheme
			reconciler := &OldProviderConnectionReconciler{
				Client: fakeClient,
				Log:    ctrl.Log.WithName("controllers").WithName("OldProviderConnectionReconciler"),
				Scheme: s,
			}

			req := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      secretName,
					Namespace: secretNamespace,
				},
			}
			_, err := reconciler.Reconcile(context.Background(), req)
			tt.validateFunc(fakeClient, err, t)
		})
	}
}

func newSecret(namespace, name string, labels map[string]string, metadata string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Data: map[string][]byte{
			"metadata": []byte(metadata),
		},
	}
}
