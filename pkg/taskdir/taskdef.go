package taskdir

import (
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/airplanedev/cli/pkg/logger"
	"github.com/airplanedev/cli/pkg/taskdir/definitions"
	"github.com/airplanedev/cli/pkg/utils"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v3"
)

func (this TaskDirectory) ReadDefinition() (definitions.Definition, error) {
	buf, err := ioutil.ReadFile(this.defPath)
	if err != nil {
		return definitions.Definition{}, errors.Wrap(err, "reading task definition")
	}

	defPath := this.defPath
	// Attempt to set a prettier defPath, best effort
	if wd, err := os.Getwd(); err != nil {
		logger.Debug("%s", err)
	} else if path, err := filepath.Rel(wd, defPath); err != nil {
		logger.Debug("%s", err)
	} else {
		defPath = path
	}

	return definitions.UnmarshalDefinition(buf, defPath)
}

// WriteSlug updates the slug of a task definition and persists this to disk.
//
// It attempts to retain the existing file's formatting (comments, etc.) where possible.
func (this TaskDirectory) WriteSlug(slug string) error {
	if err := utils.SetYAMLField(this.defPath, "slug", slug); err != nil {
		return errors.Wrap(err, "setting slug")
	}

	return nil
}

func (this TaskDirectory) WriteDefinition(def definitions.Definition) error {
	data, err := yaml.Marshal(def)
	if err != nil {
		return errors.Wrap(err, "marshalling definition")
	}

	if err := ioutil.WriteFile(this.defPath, data, 0664); err != nil {
		return errors.Wrap(err, "writing file")
	}

	return nil
}
