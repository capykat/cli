package build

import (
	_ "embed"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/airplanedev/cli/pkg/api"
	"github.com/airplanedev/cli/pkg/fsx"
	"github.com/pkg/errors"
)

// Python creates a dockerfile for Python.
func python(root string, args api.KindOptions) (string, error) {
	if args["shim"] != "true" {
		return pythonLegacy(root, args)
	}

	// Assert that the entrypoint file exists:
	entrypoint, _ := args["entrypoint"].(string)
	if err := fsx.AssertExistsAll(filepath.Join(root, entrypoint)); err != nil {
		return "", err
	}

	v, err := GetVersion(NamePython, "3")
	if err != nil {
		return "", err
	}

	shim, err := PythonShim(entrypoint)
	if err != nil {
		return "", err
	}

	const dockerfile = `
    FROM {{ .Base }}
    WORKDIR /airplane
    RUN mkdir -p .airplane && echo '{{.Shim}}' > .airplane/shim.py
    {{if .HasRequirements}}
    COPY requirements.txt .
    RUN pip install -r requirements.txt
    {{end}}
    COPY . .
    ENTRYPOINT ["python", ".airplane/shim.py"]
	`

	df, err := applyTemplate(dockerfile, struct {
		Base            string
		Shim            string
		HasRequirements bool
	}{
		Base:            v.String(),
		Shim:            strings.Join(strings.Split(shim, "\n"), "\\n\\\n"),
		HasRequirements: fsx.Exists(filepath.Join(root, "requirements.txt")),
	})
	if err != nil {
		return "", errors.Wrapf(err, "rendering dockerfile")
	}

	return df, nil
}

//go:embed python-shim.py
var pythonShim string

// PythonShim generates a shim file for running Python tasks.
func PythonShim(entrypoint string) (string, error) {
	shim, err := applyTemplate(pythonShim, struct {
		Entrypoint string
	}{
		Entrypoint: entrypoint,
	})
	if err != nil {
		return "", errors.Wrapf(err, "rendering shim")
	}

	return shim, nil
}

// PythonLegacy generates a dockerfile for legacy python support.
func pythonLegacy(root string, args api.KindOptions) (string, error) {
	var entrypoint, _ = args["entrypoint"].(string)
	var main = filepath.Join(root, entrypoint)
	var reqs = filepath.Join(root, "requirements.txt")

	if err := fsx.AssertExistsAll(main); err != nil {
		return "", err
	}

	t, err := template.New("python").Parse(`
    FROM {{ .Base }}
    WORKDIR /airplane
		{{if not .HasRequirements}}
		RUN echo > requirements.txt
		{{end}}
    COPY . .
    RUN pip install -r requirements.txt
    ENTRYPOINT ["python", "/airplane/{{ .Entrypoint }}"]
	`)
	if err != nil {
		return "", err
	}

	v, err := GetVersion(NamePython, "3")
	if err != nil {
		return "", err
	}

	var buf strings.Builder
	if err := t.Execute(&buf, struct {
		Base            string
		Entrypoint      string
		HasRequirements bool
	}{
		Base:            v.String(),
		Entrypoint:      entrypoint,
		HasRequirements: fsx.AssertExistsAll(reqs) == nil,
	}); err != nil {
		return "", err
	}

	return buf.String(), nil
}
