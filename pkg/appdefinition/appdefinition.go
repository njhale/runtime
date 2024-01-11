package appdefinition

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/acorn-io/aml"
	"github.com/acorn-io/aml/pkg/eval"
	"github.com/acorn-io/aml/pkg/value"
	"github.com/acorn-io/baaah/pkg/typed"
	v1 "github.com/acorn-io/runtime/pkg/apis/internal.acorn.io/v1"
	"sigs.k8s.io/yaml"
)

const (
	IconFile         = "icon"
	ReadmeFile       = "README"
	Acornfile        = "Acornfile"
	ImageDataFile    = "images.json"
	VersionFile      = "version.json"
	VCSDataFile      = "vcs.json"
	BuildDataFile    = "build.json"
	BuildContextFile = "build-context.json"
	messageSuffix    = ", you may need to define the image/build in the images section of the Acornfile"

	AcornfileSchemaVersion = "v1"
)

var (
	ErrInvalidInput = errors.New("invalid input")
	schema          value.Schema
	schemaOnce      sync.Once
)

func init() {
	// Disable AML debug printing
	eval.DebugEnabled = false
}

func getSchema() value.Schema {
	schemaOnce.Do(func() {
		f, err := fs.Open(schemaFile)
		if err != nil {
			// this shouldn't happen, this an embedded FS
			panic(err)
		}
		defer f.Close()

		err = aml.NewDecoder(f, aml.DecoderOption{
			SchemaSourceName: schemaFile,
		}).Decode(&schema)
		if err != nil {
			panic(err)
		}
	})
	return schema
}

type DataFiles struct {
	IconSuffix string
	Icon       []byte
	Readme     []byte
}

type AppDefinition struct {
	data         []byte
	imageDatas   []v1.ImagesData
	hasImageData bool
	args         map[string]any
	profiles     []string
}

func FromAppImage(appImage *v1.AppImage) (appDef *AppDefinition, err error) {
	appDef, err = NewAppDefinition([]byte(appImage.Acornfile))
	if err != nil {
		return nil, err
	}

	appDef = appDef.WithImageData(appImage.ImageData)
	return appDef, err
}

func (a *AppDefinition) clone() AppDefinition {
	return AppDefinition{
		data:         a.data,
		imageDatas:   a.imageDatas,
		hasImageData: a.hasImageData,
		args:         a.args,
		profiles:     a.profiles,
	}
}

func (a *AppDefinition) ClearImageData() (_ *AppDefinition, images *v1.ImagesData) {
	if a.hasImageData {
		data := a.imagesData()
		images = &data
	}
	result := a.clone()
	result.hasImageData = false
	result.imageDatas = nil
	return &result, images
}

func (a *AppDefinition) WithImageData(imageData v1.ImagesData) *AppDefinition {
	result := a.clone()
	result.hasImageData = true
	result.imageDatas = append(result.imageDatas, imageData)
	return &result
}

func NewAppDefinition(data []byte) (*AppDefinition, error) {
	appDef := &AppDefinition{
		data: data,
	}
	_, err := appDef.AppSpec()
	if err != nil {
		return nil, err
	}
	return appDef, nil
}

func assignAcornImage(originalImage string, build *v1.AcornBuild, image string) (string, *v1.AcornBuild) {
	if build == nil {
		build = &v1.AcornBuild{}
	}
	if build.OriginalImage == "" {
		build.OriginalImage = originalImage
	}
	return image, build
}

func assignImage(originalImage string, build *v1.Build, image string) (string, *v1.Build) {
	if build == nil {
		build = &v1.Build{
			Context:    ".",
			Dockerfile: "Dockerfile",
		}
	}
	if build.BaseImage == "" {
		build.BaseImage = originalImage
	} else if build.BaseImage == originalImage {
		build.BaseImage = image
	}
	return image, build
}

func (a *AppDefinition) WithArgs(args map[string]any, profiles []string) *AppDefinition {
	result := a.clone()
	result.args = args
	result.profiles = profiles
	return &result
}

func (a *AppDefinition) YAML() (string, error) {
	jsonData, err := a.JSON()
	if err != nil {
		return "", err
	}
	data := map[string]any{}
	if err := json.Unmarshal([]byte(jsonData), &data); err != nil {
		return "", err
	}
	y, err := yaml.Marshal(data)
	return string(y), err
}

func (a *AppDefinition) JSON() (string, error) {
	appSpec, err := a.AppSpec()
	if err != nil {
		return "", err
	}
	app, err := json.MarshalIndent(appSpec, "", "  ")
	return string(app), err
}

func (a *AppDefinition) getData() []byte {
	def, err := fs.ReadFile(defaultFile)
	if err != nil {
		panic(err)
	}
	return append(a.data, def...)
}

func (a *AppDefinition) decode(out any) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	return aml.NewDecoder(bytes.NewReader(a.getData()), aml.DecoderOption{
		Context:          ctx,
		SourceName:       "Acornfile",
		Args:             a.args,
		Profiles:         a.profiles,
		SchemaSourceName: "acornfile-schema.acorn",
		SchemaValue:      getSchema(),
	}).Decode(out)
}

func (a *AppDefinition) imagesData() (result v1.ImagesData) {
	for _, imageData := range a.imageDatas {
		result.Containers = typed.Concat(result.Containers, imageData.Containers)
		result.Functions = typed.Concat(result.Functions, imageData.Functions)
		result.Jobs = typed.Concat(result.Jobs, imageData.Jobs)
		result.Images = typed.Concat(result.Images, imageData.Images)
		result.Acorns = typed.Concat(result.Acorns, imageData.Acorns)
		result.Builds = append(result.Builds, imageData.Builds...)
	}
	return
}

func getFragment(imagesData v1.ImagesData, imageID string) string {
	for _, check := range []map[string]v1.ContainerData{
		imagesData.Containers,
		imagesData.Functions,
		imagesData.Jobs,
	} {
		for _, imageData := range check {
			if imageData.Image == imageID && imageData.AcornfileFragment != "" {
				return imageData.AcornfileFragment
			}
		}
	}
	return ""
}

func overlayFragment(con *v1.Container, imagesData v1.ImagesData, imageID, serviceName string) error {
	fragment := getFragment(imagesData, imageID)
	if fragment == "" {
		return nil
	}

	containerData, err := json.Marshal(con)
	if err != nil {
		return err
	}

	strData := fmt.Sprintf(`
let serviceName: "%s"
let base: { %s }
{ %s } + { %s }`, serviceName, containerData, containerData, fragment)

	return aml.Unmarshal([]byte(strData), con)
}

func (a *AppDefinition) AppSpec() (*v1.AppSpec, error) {
	spec := &v1.AppSpec{}
	if err := a.decode(spec); err != nil {
		return nil, err
	}

	if !a.hasImageData {
		return spec, nil
	}

	imagesData := a.imagesData()

	for containerName, conSpec := range spec.Containers {
		if image, ok := GetImageReferenceForServiceName(containerName, spec, imagesData); ok {
			if err := overlayFragment(&conSpec, imagesData, image, containerName); err != nil {
				return nil, err
			}
			conSpec.Image, conSpec.Build = assignImage(conSpec.Image, conSpec.Build, image)
		} else {
			return nil, fmt.Errorf("failed to find image for container [%s] in Acornfile"+messageSuffix, containerName)
		}
		for sidecarName, sidecarSpec := range conSpec.Sidecars {
			if image, ok := GetImageReferenceForServiceName(containerName+"."+sidecarName, spec, imagesData); ok {
				sidecarSpec.Image, sidecarSpec.Build = assignImage(sidecarSpec.Image, sidecarSpec.Build, image)
				conSpec.Sidecars[sidecarName] = sidecarSpec
			} else {
				return nil, fmt.Errorf("failed to find image for sidecar [%s] in container [%s] in Acornfile"+messageSuffix, sidecarName, containerName)
			}
		}
		spec.Containers[containerName] = conSpec
	}

	for functionName, conSpec := range spec.Functions {
		if image, ok := GetImageReferenceForServiceName(functionName, spec, imagesData); ok {
			if err := overlayFragment(&conSpec, imagesData, image, functionName); err != nil {
				return nil, err
			}
			conSpec.Image, conSpec.Build = assignImage(conSpec.Image, conSpec.Build, image)
		} else {
			return nil, fmt.Errorf("failed to find image for function [%s] in Acornfile"+messageSuffix, functionName)
		}
		for sidecarName, sidecarSpec := range conSpec.Sidecars {
			if image, ok := GetImageReferenceForServiceName(functionName+"."+sidecarName, spec, imagesData); ok {
				sidecarSpec.Image, sidecarSpec.Build = assignImage(sidecarSpec.Image, sidecarSpec.Build, image)
				conSpec.Sidecars[sidecarName] = sidecarSpec
			} else {
				return nil, fmt.Errorf("failed to find image for sidecar [%s] in function [%s] in Acornfile"+messageSuffix, sidecarName, functionName)
			}
		}
		spec.Functions[functionName] = conSpec
	}

	for containerName, conSpec := range spec.Jobs {
		if image, ok := GetImageReferenceForServiceName(containerName, spec, imagesData); ok {
			if err := overlayFragment(&conSpec, imagesData, image, containerName); err != nil {
				return nil, err
			}
			conSpec.Image, conSpec.Build = assignImage(conSpec.Image, conSpec.Build, image)
		} else {
			return nil, fmt.Errorf("failed to find image for job [%s] in Acornfile"+messageSuffix, containerName)
		}
		for sidecarName, sidecarSpec := range conSpec.Sidecars {
			if image, ok := GetImageReferenceForServiceName(containerName+"."+sidecarName, spec, imagesData); ok {
				sidecarSpec.Image, sidecarSpec.Build = assignImage(sidecarSpec.Image, sidecarSpec.Build, image)
				conSpec.Sidecars[sidecarName] = sidecarSpec
			} else {
				return nil, fmt.Errorf("failed to find image for sidecar [%s] in job [%s] in Acornfile"+messageSuffix, sidecarName, containerName)
			}
		}
		spec.Jobs[containerName] = conSpec
	}

	for imageName, imgSpec := range spec.Images {
		if image, ok := GetImageReferenceForServiceName(imageName, spec, imagesData); ok {
			if imgSpec.AcornBuild != nil {
				imgSpec.Image, imgSpec.AcornBuild = assignAcornImage(imgSpec.Image, imgSpec.AcornBuild, image)
			} else {
				imgSpec.Image, imgSpec.Build = assignImage(imgSpec.Image, imgSpec.Build, image)
			}
		} else {
			return nil, fmt.Errorf("failed to find image for image definition [%s] in Acornfile"+messageSuffix, imageName)
		}
		spec.Images[imageName] = imgSpec
	}

	for acornName, acornSpec := range spec.Acorns {
		if image, ok := GetImageReferenceForServiceName(acornName, spec, imagesData); ok {
			acornSpec.Image, acornSpec.Build = assignAcornImage(acornSpec.Image, acornSpec.Build, image)
		} else {
			return nil, fmt.Errorf("failed to find image for acorn [%s] in Acornfile"+messageSuffix, acornName)
		}
		spec.Acorns[acornName] = acornSpec
	}

	for serviceName, serviceSpec := range spec.Services {
		if serviceSpec.Image == "" && serviceSpec.Build == nil {
			continue
		}
		if image, ok := GetImageReferenceForServiceName(serviceName, spec, imagesData); ok {
			serviceSpec.Image, serviceSpec.Build = assignAcornImage(serviceSpec.Image, serviceSpec.Build, image)
		} else {
			return nil, fmt.Errorf("failed to find image for service [%s] in Acornfile"+messageSuffix, serviceName)
		}
		spec.Services[serviceName] = serviceSpec
	}

	return spec, v1.AddImpliedResources(spec)
}

func addFunctionFiles(fileSet map[string]bool, builds map[string]v1.ContainerImageBuilderSpec, cwd string) {
	for key, build := range builds {
		addContainerFiles(fileSet, build.Sidecars, cwd)
		context := build.Src
		if context == "" {
			context = key
		}
		fileSet[filepath.Join(cwd, context, "build.acorn")] = true
		fileSet[filepath.Join(cwd, context, ".dockerignore")] = true
	}
}

func addContainerFiles(fileSet map[string]bool, builds map[string]v1.ContainerImageBuilderSpec, cwd string) {
	for _, build := range builds {
		addContainerFiles(fileSet, build.Sidecars, cwd)
		if build.Build == nil || build.Build.BaseImage != "" {
			continue
		}
		fileSet[filepath.Join(cwd, build.Build.Dockerfile)] = true
		fileSet[filepath.Join(filepath.Dir(filepath.Join(cwd, build.Build.Dockerfile)), ".dockerignore")] = true
	}
}

func addAcorns(fileSet map[string]bool, builds map[string]v1.AcornBuilderSpec, cwd string) {
	for _, build := range builds {
		if build.Build == nil {
			continue
		}
		data, err := aml.ReadFile(filepath.Join(cwd, build.Build.Acornfile))
		if err != nil {
			return
		}

		fileSet[filepath.Join(cwd, build.Build.Acornfile)] = true

		appDef, err := NewAppDefinition(data)
		if err != nil {
			return
		}
		files, err := appDef.WatchFiles(filepath.Join(cwd, build.Build.Context))
		if err != nil {
			return
		}
		for _, f := range files {
			fileSet[f] = true
		}
	}
}

func addFiles(fileSet map[string]bool, builds map[string]v1.ImageBuilderSpec, cwd string) {
	for _, build := range builds {
		if build.ContainerBuild == nil {
			if build.AcornBuild != nil {
				fileSet[filepath.Join(cwd, build.AcornBuild.Acornfile)] = true
			}
		} else {
			fileSet[filepath.Join(cwd, build.ContainerBuild.Dockerfile)] = true
		}
	}
}

func (a *AppDefinition) WatchFiles(cwd string) (result []string, _ error) {
	fileSet := map[string]bool{}
	spec, err := a.BuilderSpec()
	if err != nil {
		return nil, err
	}

	addContainerFiles(fileSet, spec.Containers, cwd)
	addFunctionFiles(fileSet, spec.Functions, cwd)
	addContainerFiles(fileSet, spec.Jobs, cwd)
	addFiles(fileSet, spec.Images, cwd)
	addAcorns(fileSet, spec.Services, cwd)
	addAcorns(fileSet, spec.Acorns, cwd)

	for k := range fileSet {
		result = append(result, k)
	}
	sort.Strings(result)
	return result, nil
}

func (a *AppDefinition) BuilderSpec() (*v1.BuilderSpec, error) {
	spec := &v1.BuilderSpec{}
	return spec, a.decode(spec)
}

func IconFromTar(reader io.Reader) ([]byte, error) {
	tar := tar.NewReader(reader)
	for {
		header, err := tar.Next()
		if err == io.EOF {
			break
		}

		if header.Name == IconFile {
			return io.ReadAll(tar)
		}
	}

	return nil, nil
}

func AppImageFromTar(reader io.Reader) (*v1.AppImage, *DataFiles, error) {
	tar := tar.NewReader(reader)
	result := &v1.AppImage{}
	dataFiles := &DataFiles{}
	for {
		header, err := tar.Next()
		if err == io.EOF {
			break
		}

		switch header.Name {
		case Acornfile:
			data, err := io.ReadAll(tar)
			if err != nil {
				return nil, nil, err
			}
			result.Acornfile = string(data)
		case VersionFile:
			result.Version = &v1.AppImageVersion{}
			err := json.NewDecoder(tar).Decode(&result.Version)
			if err != nil {
				return nil, nil, err
			}
		case ImageDataFile:
			err := json.NewDecoder(tar).Decode(&result.ImageData)
			if err != nil {
				return nil, nil, err
			}
		case VCSDataFile:
			err := json.NewDecoder(tar).Decode(&result.VCS)
			if err != nil {
				return nil, nil, err
			}
		case BuildDataFile:
			result.BuildArgs = v1.NewGenericMap(map[string]any{})
			err := json.NewDecoder(tar).Decode(result.BuildArgs)
			if err != nil {
				return nil, nil, err
			}
		case BuildContextFile:
			err := json.NewDecoder(tar).Decode(&result.BuildContext)
			if err != nil {
				return nil, nil, err
			}
		case ReadmeFile:
			dataFiles.Readme, err = io.ReadAll(tar)
			if err != nil {
				return nil, nil, err
			}
		default:
			if strings.HasPrefix(header.Name, IconFile) {
				dataFiles.Icon, err = io.ReadAll(tar)
				if err != nil {
					return nil, nil, err
				}
				dataFiles.IconSuffix = strings.TrimPrefix(header.Name, IconFile)
			}
		}
	}

	if result.Acornfile == "" {
		return nil, nil, fmt.Errorf("invalid image no Acornfile found")
	}

	return result, dataFiles, nil
}
