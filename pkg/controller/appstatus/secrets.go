package appstatus

import (
	"fmt"
	"strconv"

	"github.com/acorn-io/baaah/pkg/router"
	"github.com/acorn-io/baaah/pkg/typed"
	v1 "github.com/acorn-io/runtime/pkg/apis/internal.acorn.io/v1"
	"github.com/acorn-io/runtime/pkg/labels"
	"github.com/acorn-io/runtime/pkg/publicname"
	"github.com/acorn-io/runtime/pkg/ref"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

func linkedSecret(app *v1.AppInstance, name string) string {
	if name == "" {
		return ""
	}

	for _, binding := range app.Spec.Secrets {
		if binding.Target == name {
			return binding.Secret
		}
	}

	return ""
}

func (a *appStatusRenderer) readSecrets() (err error) {
	var (
		existingStatus = a.app.Status.AppStatus.Secrets
	)
	// reset state
	a.app.Status.AppStatus.Secrets = map[string]v1.SecretStatus{}

	for secretName, secretDef := range a.app.Status.AppSpec.Secrets {
		s := v1.SecretStatus{
			CommonStatus: v1.CommonStatus{
				LinkOverride:          linkedSecret(a.app, secretName),
				ErrorMessages:         existingStatus[secretName].LookupErrors,
				TransitioningMessages: existingStatus[secretName].LookupTransitioning,
			},
		}

		secret := &corev1.Secret{}
		if err := ref.Lookup(a.ctx, a.c, secret, a.app.Status.Namespace, secretName); apierrors.IsNotFound(err) {
			a.app.Status.AppStatus.Secrets[secretName] = s
			continue
		} else if err != nil {
			return err
		}

		s.UpToDate = secret.Annotations[labels.AcornAppGeneration] == strconv.Itoa(int(a.app.Generation))
		s.Defined = true
		s.Ready = true

		sourceSecret := &corev1.Secret{}
		if err := a.c.Get(a.ctx, router.Key(secret.Labels[labels.AcornSecretSourceNamespace], secret.Labels[labels.AcornSecretSourceName]), sourceSecret); apierrors.IsNotFound(err) {
			s.State = "waiting"
			a.app.Status.AppStatus.Secrets[secretName] = s
			continue
		} else if err != nil {
			return err
		}

		s.SecretName = publicname.Get(sourceSecret)
		if secretDef.Type == string(v1.SecretTypeGenerated) && secretDef.Params.Data["job"] != "" {
			s.JobName = fmt.Sprint(secretDef.Params.Data["job"])
			s.JobReady, err = a.isJobReady(s.JobName)
			if err != nil {
				return err
			}
		} else {
			s.JobReady = true
		}

		s.Ready = s.Ready && s.JobReady
		s.DataKeys = typed.SortedKeys(sourceSecret.Data)

		// Not ready if we have any error messages
		if len(s.ErrorMessages) > 0 {
			s.Ready = false
		}

		if s.Ready {
			s.State = "created"
		} else if s.UpToDate {
			if len(s.ErrorMessages) > 0 {
				s.State = "failing"
			} else {
				s.State = "updating"
			}
		} else if s.Defined {
			if len(s.ErrorMessages) > 0 {
				s.State = "error"
			} else {
				s.State = "updating"
			}
		} else {
			if len(s.ErrorMessages) > 0 {
				s.State = "error"
			} else {
				s.State = "pending"
			}
		}

		a.app.Status.AppStatus.Secrets[secretName] = s
	}

	return nil
}
