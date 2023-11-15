package client

import (
	"context"
	"errors"
	"sort"
	"strings"

	apiv1 "github.com/acorn-io/runtime/pkg/apis/api.acorn.io/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func (c *DefaultClient) SecretCreate(ctx context.Context, name, secretType string, data map[string][]byte) (*apiv1.Secret, error) {
	secret := &apiv1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: c.Namespace,
		},
		Type: secretType,
		Data: data,
	}
	if strings.HasSuffix(secret.Name, "-") {
		secret.GenerateName = secret.Name
		secret.Name = ""
	}
	return secret, c.Client.Create(ctx, secret)
}

func (c *DefaultClient) SecretGet(ctx context.Context, name string) (*apiv1.Secret, error) {
	secret := &apiv1.Secret{}
	return secret, c.Client.Get(ctx, kclient.ObjectKey{
		Name:      name,
		Namespace: c.Namespace,
	}, secret)
}

func (c *DefaultClient) SecretReveal(ctx context.Context, name string) (*apiv1.Secret, error) {
	result := &apiv1.Secret{}
	err := c.RESTClient.Get().
		Namespace(c.Namespace).
		Resource("secrets").
		Name(name).
		SubResource("reveal").
		Do(ctx).Into(result)
	return result, err
}

func (c *DefaultClient) SecretUpdate(ctx context.Context, name string, data map[string][]byte) (*apiv1.Secret, error) {
	secret := &apiv1.Secret{}
	err := c.Client.Get(ctx, kclient.ObjectKey{
		Name:      name,
		Namespace: c.Namespace,
	}, secret)
	if err != nil {
		return nil, err
	}

	secret.Data = data
	return secret, c.Client.Update(ctx, secret)
}

func (c *DefaultClient) SecretList(ctx context.Context) ([]apiv1.Secret, error) {
	result := &apiv1.SecretList{}
	err := c.Client.List(ctx, result, &kclient.ListOptions{
		Namespace: c.Namespace,
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(result.Items, func(i, j int) bool {
		if result.Items[i].CreationTimestamp.Time == result.Items[j].CreationTimestamp.Time {
			return result.Items[i].Name < result.Items[j].Name
		}
		return result.Items[i].CreationTimestamp.After(result.Items[j].CreationTimestamp.Time)
	})

	return result.Items, nil
}

func (c *DefaultClient) SecretDelete(ctx context.Context, name string) (*apiv1.Secret, error) {
	secret, err := c.SecretGet(ctx, name)
	if apierrors.IsNotFound(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}

	err = c.Client.Delete(ctx, &apiv1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secret.Name,
			Namespace: secret.Namespace,
		},
	})
	if apierrors.IsNotFound(err) {
		return secret, nil
	}
	return secret, err
}

func (c *DefaultClient) SecretsDelete(ctx context.Context, name string) ([]apiv1.Secret, error) {
	var secrets []apiv1.Secret
	if matches, wildcard := WildcardMatcher(name); wildcard {
		all, err := c.SecretList(ctx)
		if err != nil {
			return nil, err
		}

		for _, s := range all {
			if matches(s.Name) {
				secrets = append(secrets, s)
			}
		}
	} else {
		s, err := c.SecretGet(ctx, name)
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}

		secrets = append(secrets, *s)
	}

	var (
		deleted []apiv1.Secret
		errs    []error
	)
	for _, s := range secrets {
		if err := c.Client.Delete(ctx, &apiv1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      s.Name,
				Namespace: c.Namespace,
			},
		}); !apierrors.IsNotFound(err) && err != nil {
			errs = append(errs, err)
			continue
		}

		deleted = append(deleted, s)
	}

	return deleted, errors.Join(errs...)
}
