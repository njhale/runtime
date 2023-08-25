package appdefinition

import (
	"github.com/acorn-io/baaah/pkg/router"
	v1 "github.com/acorn-io/runtime/pkg/apis/internal.acorn.io/v1"
	"github.com/acorn-io/runtime/pkg/appdefinition"
	"github.com/acorn-io/runtime/pkg/condition"
)

func ParseAppImage(req router.Request, resp router.Response) error {
	appInstance := req.Object.(*v1.AppInstance)
	status := condition.Setter(appInstance, resp, v1.AppInstanceConditionParsed)
	appImage := appInstance.Status.AppImage

	if appImage.Acornfile == "" {
		return nil
	}

	appDef, err := appdefinition.FromAppImage(&appImage)
	if err != nil {
		status.Error(err)
		return nil
	}

	var args map[string]any
	if appInstance.Spec.DeployArgs != nil {
		args = appInstance.Spec.DeployArgs.Data
	}
	appDef, _, err = appDef.WithArgs(args, appInstance.Spec.GetProfiles(appInstance.Status.GetDevMode()))
	if err != nil {
		status.Error(err)
		return nil
	}

	appSpec, err := appDef.AppSpec()
	if err != nil {
		status.Error(err)
		return nil
	}

	appInstance.Status.AppSpec = *appSpec
	status.Success()
	return nil
}
