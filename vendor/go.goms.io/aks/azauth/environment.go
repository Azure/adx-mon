package azauth

import (
	"errors"
	"fmt"

	"github.com/Azure/go-autorest/autorest/azure"
)

// EnvironmentLoader is responsible for loading an Azure Environment config into memory.
//
// See:
//	- func EnvironmentChain      - statically define all possible ways to load an environment config.
//	- func EnvironmentFromName   - load an environment config based on the environment name
//  - func EnvironmentFromFile   - load an environment config from a file.
//  - func EnvironmentFromURL    - load an environment config from a URL.
//  - func EnvironmentFromStruct - use an environment that has already been loaded.
type EnvironmentLoader interface {
	GetEnvironment() (azure.Environment, error)
}

type EnvironmentLoaderFunc func() (azure.Environment, error)

func (f EnvironmentLoaderFunc) GetEnvironment() (azure.Environment, error) {
	return f()
}

// EnvironmentChain returns an EnvironmentLoader that attempts to load a single Azure Environment by consulting many
// different EnvironmentLoaders.
//
// The first EnvironmentLoader that returns a valid Azure environment config causes the chain to short-circuit and
// return.
func EnvironmentChain(loaders ...EnvironmentLoader) EnvironmentLoader {
	return &environmentLoaderChain{loaders: loaders}
}

type environmentLoaderChain struct {
	loaders []EnvironmentLoader
}

func (e environmentLoaderChain) GetEnvironment() (azure.Environment, error) {
	var (
		env azure.Environment
		err error
	)

	if len(e.loaders) == 0 {
		return env, errors.New("no environment loaders provided")
	}

	var misses []error

	for _, loader := range e.loaders {
		loader := loader

		env, err = loader.GetEnvironment()
		if err != nil {
			misses = append(misses, err)
		}

		if (env != azure.Environment{}) {
			return env, nil
		}
	}

	return env, errors.New(fmtEnvChainErrorMessage(misses))
}

func EnvironmentFromName(name string) EnvironmentLoader {
	return fromName{name: name}
}

func EnvironmentFromFile(file string) EnvironmentLoader {
	return fromFile{file: file}
}

// EnvironmentFromStruct returns a known environment struct that has been previously loaded. It is useful when you want
// to leverage the azauth Setup func but already have loaded the environment and do not need or want the library
// to perform discovery.
//
// The EnvironmentLoader returned from this func CANNOT error so it is safe to swallow the error.
func EnvironmentFromStruct(environment azure.Environment) EnvironmentLoader {
	return EnvironmentLoaderFunc(func() (azure.Environment, error) {
		return environment, nil
	})
}

// EnvironmentFromURL loads an Environment from a URL. The URL is actually the Azure Stack /metadata/endpoints resource
// and is not a full Environment document. The Azure SDK synthesizes an Environment from the retrieved endpoints
// document.
func EnvironmentFromURL(url string, overrides ...azure.OverrideProperty) EnvironmentLoader {
	if overrides == nil {
		overrides = make([]azure.OverrideProperty, 0)
	}

	return fromURL{url: url, overrides: overrides}
}

type fromName struct {
	name string
}

func (f fromName) GetEnvironment() (azure.Environment, error) {
	var (
		env azure.Environment
		err error
	)

	env, err = azure.EnvironmentFromName(f.name)
	if err != nil {
		return env, fmt.Errorf("EnvironmentFromName(%q) - failed to load: %v", f.name, err)
	}

	return env, nil
}

type fromFile struct {
	file string
}

func (f fromFile) GetEnvironment() (azure.Environment, error) {
	var (
		env azure.Environment
		err error
	)

	env, err = azure.EnvironmentFromFile(f.file)
	if err != nil {
		return env, fmt.Errorf("EnvironmentFromFile(%q) - failed to load: %v", f.file, err)
	}

	return env, nil
}

type fromURL struct {
	url       string
	overrides []azure.OverrideProperty
}

func (f fromURL) GetEnvironment() (azure.Environment, error) {
	var (
		env azure.Environment
		err error
	)

	env, err = azure.EnvironmentFromURL(f.url, f.overrides...)
	if err != nil {
		return env, fmt.Errorf("EnvironmentFromURL(%q) - failed to load: %v", f.url, err)
	}

	return env, nil
}

func fmtEnvChainErrorMessage(errors []error) string {
	msg := fmt.Sprintf("failed to load environment from chain (%d attempts)\n", len(errors))

	for attempt, err := range errors {
		attemptLine := fmt.Sprintf("\t%d. %s\n", attempt+1, err) //nolint:gomnd
		msg += attemptLine
	}

	return msg
}
