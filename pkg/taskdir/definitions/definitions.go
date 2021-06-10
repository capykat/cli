package definitions

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/airplanedev/cli/pkg/api"
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v3"
)

// Definition represents a YAML-based task definition that can be used to create
// or update Airplane tasks.
//
// Note this is the subset of fields that can be represented with a revision,
// and therefore isolated to a specific environment.
type Definition Definition_0_2

func NewDefinitionFromTask(task api.Task) (Definition, error) {
	def := Definition{
		Slug:             task.Slug,
		Name:             task.Name,
		Description:      task.Description,
		Arguments:        task.Arguments,
		Parameters:       task.Parameters,
		Constraints:      task.Constraints,
		Env:              task.Env,
		ResourceRequests: task.ResourceRequests,
		Repo:             task.Repo,
		Timeout:          task.Timeout,
	}

	var taskDef interface{}
	if task.Kind == api.TaskKindDeno {
		def.Deno = &DenoDefinition{}
		taskDef = &def.Deno

	} else if task.Kind == api.TaskKindDockerfile {
		def.Dockerfile = &DockerfileDefinition{}
		taskDef = &def.Dockerfile

	} else if task.Kind == api.TaskKindGo {
		def.Go = &GoDefinition{}
		taskDef = &def.Go

	} else if task.Kind == api.TaskKindNode {
		def.Node = &NodeDefinition{}
		taskDef = &def.Node

	} else if task.Kind == api.TaskKindPython {
		def.Python = &PythonDefinition{}
		taskDef = &def.Python

	} else if task.Kind == api.TaskKindImage {
		def.Image = &ImageDefinition{
			Command: task.Command,
		}
		if task.Image != nil {
			def.Image.Image = *task.Image
		}

	} else if task.Kind == api.TaskKindSQL {
		def.SQL = &SQLDefinition{}
		taskDef = &def.SQL

	} else if task.Kind == api.TaskKindREST {
		def.REST = &RESTDefinition{}
		taskDef = &def.REST

	} else {
		return Definition{}, errors.Errorf("unknown kind specified: %s", task.Kind)
	}

	if taskDef != nil {
		if err := mapstructure.Decode(task.KindOptions, taskDef); err != nil {
			return Definition{}, errors.Wrap(err, "decoding options")
		}
	}

	return def, nil
}

func (def Definition) GetKindAndOptions() (api.TaskKind, api.KindOptions, error) {
	options := api.KindOptions{}
	if def.Deno != nil {
		if err := mapstructure.Decode(def.Deno, &options); err != nil {
			return "", api.KindOptions{}, errors.Wrap(err, "decoding Deno definition")
		}
		return api.TaskKindDeno, options, nil
	} else if def.Dockerfile != nil {
		if err := mapstructure.Decode(def.Dockerfile, &options); err != nil {
			return "", api.KindOptions{}, errors.Wrap(err, "decoding Dockerfile definition")
		}
		return api.TaskKindDockerfile, options, nil
	} else if def.Image != nil {
		return api.TaskKindImage, api.KindOptions{}, nil
	} else if def.Go != nil {
		if err := mapstructure.Decode(def.Go, &options); err != nil {
			return "", api.KindOptions{}, errors.Wrap(err, "decoding Go definition")
		}
		return api.TaskKindGo, options, nil
	} else if def.Node != nil {
		if err := mapstructure.Decode(def.Node, &options); err != nil {
			return "", api.KindOptions{}, errors.Wrap(err, "decoding Node definition")
		}
		return api.TaskKindNode, options, nil
	} else if def.Python != nil {
		if err := mapstructure.Decode(def.Python, &options); err != nil {
			return "", api.KindOptions{}, errors.Wrap(err, "decoding Python definition")
		}
		return api.TaskKindPython, options, nil
	} else if def.SQL != nil {
		if err := mapstructure.Decode(def.SQL, &options); err != nil {
			return "", api.KindOptions{}, errors.Wrap(err, "decoding SQL definition")
		}
		return api.TaskKindSQL, options, nil
	} else if def.REST != nil {
		if err := mapstructure.Decode(def.REST, &options); err != nil {
			return "", api.KindOptions{}, errors.Wrap(err, "decoding REST definition")
		}
		// API expects jsonBody to be a string, since it's handlebars-templated JSON and not always valid JSON. For
		// convenience, we allow the YAML definition to be a structured object when the jsonBody is actually valid
		// JSON. In that case, if it's not a string, we JSON-serialize it into a string.
		if _, ok := options["jsonBody"].(string); !ok && options["jsonBody"] != nil {
			jsonBody, err := json.Marshal(options["jsonBody"])
			if err != nil {
				return "", api.KindOptions{}, errors.Wrap(err, "marshalling JSON body")
			}
			options["jsonBody"] = string(jsonBody)
		}
		return api.TaskKindREST, options, nil
	}

	return "", api.KindOptions{}, errors.New("No kind specified")
}

func (def Definition) Validate() (Definition, error) {
	if def.Slug == "" {
		return def, errors.New("Expected a task slug")
	}

	defs := []string{}
	if def.Deno != nil {
		defs = append(defs, "deno")
	}
	if def.Dockerfile != nil {
		defs = append(defs, "dockerfile")
	}
	if def.Image != nil {
		defs = append(defs, "image")
	}
	if def.Go != nil {
		defs = append(defs, "go")
	}
	if def.Node != nil {
		defs = append(defs, "node")
	}
	if def.Python != nil {
		defs = append(defs, "python")
	}
	if def.SQL != nil {
		defs = append(defs, "sql")
	}
	if def.REST != nil {
		defs = append(defs, "rest")
	}

	if len(defs) == 0 {
		return def, errors.New("No task type defined")
	}
	if len(defs) > 1 {
		return def, errors.Errorf("Too many task types defined: only one of (%s) expected", strings.Join(defs, ", "))
	}

	// TODO: validate the rest of the fields!

	return def, nil
}

func UnmarshalDefinition(buf []byte, defPath string) (Definition, error) {
	// Validate definition against our Definition struct
	if err := validateYAML(buf, Definition{}); err != nil {
		// Try older definitions?
		if def, oerr := tryOlderDefinitions(buf); oerr == nil {
			return def, nil
		}

		// Print any "expected" validation errors
		switch err := errors.Cause(err).(type) {
		case ErrInvalidYAML:
			return Definition{}, newErrReadDefinition(fmt.Sprintf("Error reading %s, invalid YAML:\n  %s", defPath, err))
		case ErrSchemaValidation:
			errorMsgs := []string{}
			for _, verr := range err.Errors {
				errorMsgs = append(errorMsgs, fmt.Sprintf("%s: %s", verr.Field(), verr.Description()))
			}
			return Definition{}, newErrReadDefinition(fmt.Sprintf("Error reading %s", defPath), errorMsgs...)
		default:
			return Definition{}, errors.Wrapf(err, "reading %s", defPath)
		}
	}

	var def Definition
	if err := yaml.Unmarshal(buf, &def); err != nil {
		return Definition{}, errors.Wrap(err, "unmarshalling task definition")
	}

	return def, nil
}

func tryOlderDefinitions(buf []byte) (Definition, error) {
	var err error
	if err = validateYAML(buf, Definition_0_1{}); err == nil {
		var def Definition_0_1
		if e := yaml.Unmarshal(buf, &def); e != nil {
			return Definition{}, err
		}
		return def.upgrade()
	}
	return Definition{}, err
}
