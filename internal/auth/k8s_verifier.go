package auth

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/grs/klaviger/internal/config"
	"github.com/grs/klaviger/internal/observability"
	"github.com/lestrrat-go/jwx/v2/jwt"
	"go.uber.org/zap"
	authzv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// KubernetesVerifier verifies tokens using Kubernetes SelfSubjectAccessReview
type KubernetesVerifier struct {
	verb      string
	resource  string
	apiGroup  string
	namespace string
	clientset *kubernetes.Clientset
	logger    *zap.Logger
}

// NewKubernetesVerifier creates a new Kubernetes verifier
func NewKubernetesVerifier(cfg *config.KubernetesConfig, logger *zap.Logger) (*KubernetesVerifier, error) {
	// Try in-cluster config first
	kubeConfig, err := rest.InClusterConfig()
	if err != nil {
		// Fall back to kubeconfig file
		loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
		configOverrides := &clientcmd.ConfigOverrides{}
		kubeConfig, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			loadingRules, configOverrides).ClientConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to load kubeconfig: %w", err)
		}
	}

	// Create clientset
	clientset, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes clientset: %w", err)
	}

	// Determine namespace
	namespace := cfg.Namespace
	if namespace == "" {
		// Try to read namespace from service account token path
		const serviceAccountNamespacePath = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"
		if data, err := os.ReadFile(serviceAccountNamespacePath); err == nil {
			namespace = strings.TrimSpace(string(data))
			logger.Info("using namespace from service account",
				zap.String("namespace", namespace),
				zap.String("path", serviceAccountNamespacePath))
		}
	}

	return &KubernetesVerifier{
		verb:      cfg.Verb,
		resource:  cfg.Resource,
		apiGroup:  cfg.APIGroup,
		namespace: namespace,
		clientset: clientset,
		logger:    logger.With(zap.String("component", "k8s_verifier")),
	}, nil
}

// Verify verifies a token using Kubernetes SelfSubjectAccessReview
func (v *KubernetesVerifier) Verify(ctx context.Context, token string) (*Claims, error) {
	start := time.Now()
	result := "success"
	defer func() {
		observability.TokenVerificationDuration.WithLabelValues("k8s", result).Observe(time.Since(start).Seconds())
	}()

	// Create a temporary config with the provided token
	config, err := rest.InClusterConfig()
	if err != nil {
		// Fall back to kubeconfig
		loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
		configOverrides := &clientcmd.ConfigOverrides{}
		config, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			loadingRules, configOverrides).ClientConfig()
		if err != nil {
			result = "error"
			return nil, fmt.Errorf("failed to load kubeconfig: %w", err)
		}
	}

	// Override with provided token
	config.BearerToken = token
	config.BearerTokenFile = "" // Clear file-based token

	// Perform SelfSubjectAccessReview with the user's token
	// This implicitly validates the token and checks authorization
	userClientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		result = "error"
		return nil, fmt.Errorf("failed to create user clientset: %w", err)
	}

	accessReview := &authzv1.SelfSubjectAccessReview{
		Spec: authzv1.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authzv1.ResourceAttributes{
				Verb:      v.verb,
				Resource:  v.resource,
				Group:     v.apiGroup,
				Namespace: v.namespace,
			},
		},
	}

	accessResult, err := userClientset.AuthorizationV1().SelfSubjectAccessReviews().Create(
		ctx, accessReview, metav1.CreateOptions{})
	if err != nil {
		result = "error"
		v.logger.Debug("access review failed", zap.Error(err))
		return nil, fmt.Errorf("access review failed: %w", err)
	}

	// Check if access is allowed
	if !accessResult.Status.Allowed {
		result = "denied"
		reason := accessResult.Status.Reason
		if reason == "" {
			reason = "access denied"
		}
		return nil, fmt.Errorf("access denied: %s", reason)
	}

	// Parse the JWT token to extract claims
	// We parse without verification since SelfSubjectAccessReview already validated it
	parsedToken, err := jwt.ParseString(token, jwt.WithVerify(false), jwt.WithValidate(false))
	if err != nil {
		result = "error"
		v.logger.Debug("failed to parse JWT token", zap.Error(err))
		return nil, fmt.Errorf("failed to parse JWT token: %w", err)
	}

	// Build claims from JWT token
	claims := &Claims{
		Subject: parsedToken.Subject(),
		Extra:   make(map[string]interface{}),
	}

	// Extract standard claims into proper struct fields
	if issuer := parsedToken.Issuer(); issuer != "" {
		claims.Issuer = issuer
	}
	if audience := parsedToken.Audience(); len(audience) > 0 {
		claims.Audience = audience
	}
	if expiry := parsedToken.Expiration(); !expiry.IsZero() {
		claims.ExpiresAt = expiry.Unix()
	}
	if issuedAt := parsedToken.IssuedAt(); !issuedAt.IsZero() {
		claims.IssuedAt = issuedAt.Unix()
	}
	if notBefore := parsedToken.NotBefore(); !notBefore.IsZero() {
		claims.NotBefore = notBefore.Unix()
	}

	// Extract Kubernetes-specific claims
	// Service account tokens typically have "kubernetes.io/serviceaccount/namespace" and similar claims
	tokenMap, err := parsedToken.AsMap(ctx)
	if err == nil {
		// Add Kubernetes-specific claims
		for key, value := range tokenMap {
			// Skip standard claims we already extracted
			if key == "sub" || key == "iss" || key == "aud" || key == "exp" || key == "iat" || key == "nbf" {
				continue
			}
			// Add Kubernetes-specific claims to extra
			if strings.HasPrefix(key, "kubernetes.io/") {
				claims.Extra[key] = value
			}
		}
	}

	v.logger.Debug("kubernetes verification successful",
		zap.String("subject", claims.Subject),
		zap.Any("extra", claims.Extra),
	)

	return claims, nil
}
