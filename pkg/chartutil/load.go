package chartutil

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/golang/protobuf/ptypes/any"

	"k8s.io/helm/pkg/proto/hapi/chart"
)

// Load takes a string name, tries to resolve it to a file or directory, and then loads it.
//
// This is the preferred way to load a chart. It will discover the chart encoding
// and hand off to the appropriate chart reader.
func Load(name string) (*chart.Chart, error) {
	fi, err := os.Stat(name)
	if err != nil {
		return nil, err
	}
	if fi.IsDir() {
		return LoadDir(name)
	}
	return LoadFile(name)
}

// afile represents an archive file buffered for later processing.
type afile struct {
	name string
	data []byte
}

// LoadArchive loads from a reader containing a compressed tar archive.
func LoadArchive(in io.Reader) (*chart.Chart, error) {
	unzipped, err := gzip.NewReader(in)
	if err != nil {
		return &chart.Chart{}, err
	}
	defer unzipped.Close()

	files := []*afile{}
	tr := tar.NewReader(unzipped)
	for {
		b := bytes.NewBuffer(nil)
		hd, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return &chart.Chart{}, err
		}

		if hd.FileInfo().IsDir() {
			// Use this instead of hd.Typeflag because we don't have to do any
			// inference chasing.
			continue
		}

		parts := strings.Split(hd.Name, "/")
		n := strings.Join(parts[1:], "/")

		if _, err := io.Copy(b, tr); err != nil {
			return &chart.Chart{}, err
		}

		files = append(files, &afile{name: n, data: b.Bytes()})
		b.Reset()
	}

	if len(files) == 0 {
		return nil, errors.New("no files in chart archive")
	}

	return loadFiles(files)
}

func loadFiles(files []*afile) (*chart.Chart, error) {
	c := &chart.Chart{}
	subcharts := map[string][]*afile{}

	for _, f := range files {
		if f.name == "Chart.yaml" {
			m, err := UnmarshalChartfile(f.data)
			if err != nil {
				return c, err
			}
			c.Metadata = m
		} else if f.name == "values.toml" {
			return c, errors.New("values.toml is illegal as of 2.0.0-alpha.2")
		} else if f.name == "values.yaml" {
			c.Values = &chart.Config{Raw: string(f.data)}
		} else if strings.HasPrefix(f.name, "templates/") {
			c.Templates = append(c.Templates, &chart.Template{Name: f.name, Data: f.data})
		} else if strings.HasPrefix(f.name, "charts/") {
			cname := strings.TrimPrefix(f.name, "charts/")
			parts := strings.SplitN(cname, "/", 2)
			scname := parts[0]
			subcharts[scname] = append(subcharts[scname], &afile{name: cname, data: f.data})
		} else {
			c.Files = append(c.Files, &any.Any{TypeUrl: f.name, Value: f.data})
		}
	}

	// Ensure that we got a Chart.yaml file
	if c.Metadata == nil || c.Metadata.Name == "" {
		return c, errors.New("chart metadata (Chart.yaml) missing")
	}

	for n, files := range subcharts {
		var sc *chart.Chart
		var err error
		if filepath.Ext(n) == ".tgz" {
			file := files[0]
			if file.name != n {
				return c, fmt.Errorf("error unpacking tar in %s: expected %s, got %s", c.Metadata.Name, n, file.name)
			}
			// Untar the chart and add to c.Dependencies
			b := bytes.NewBuffer(file.data)
			sc, err = LoadArchive(b)
		} else {
			// We have to trim the prefix off of every file, and ignore any file
			// that is in charts/, but isn't actually a chart.
			buff := make([]*afile, 0, len(files))
			for _, f := range files {
				parts := strings.SplitN(f.name, "/", 2)
				if len(parts) < 2 {
					continue
				}
				f.name = parts[1]
				buff = append(buff, f)
			}
			sc, err = loadFiles(buff)
		}

		if err != nil {
			return c, fmt.Errorf("error unpacking %s in %s: %s", n, c.Metadata.Name, err)
		}

		c.Dependencies = append(c.Dependencies, sc)
	}

	return c, nil
}

// LoadFile loads from an archive file.
func LoadFile(name string) (*chart.Chart, error) {
	if fi, err := os.Stat(name); err != nil {
		return nil, err
	} else if fi.IsDir() {
		return nil, errors.New("cannot load a directory")
	}

	raw, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer raw.Close()

	return LoadArchive(raw)
}

// LoadDir loads from a directory.
//
// This loads charts only from directories.
func LoadDir(dir string) (*chart.Chart, error) {
	topdir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}

	// Just used for errors.
	c := &chart.Chart{}

	files := []*afile{}
	topdir += string(filepath.Separator)
	err = filepath.Walk(topdir, func(name string, fi os.FileInfo, err error) error {
		n := strings.TrimPrefix(name, topdir)
		if err != nil {
			return err
		}
		if fi.IsDir() {
			return nil
		}

		data, err := ioutil.ReadFile(name)
		if err != nil {
			return fmt.Errorf("error reading %s: %s", n, err)
		}

		files = append(files, &afile{name: n, data: data})
		return nil
	})
	if err != nil {
		return c, err
	}

	return loadFiles(files)
}
