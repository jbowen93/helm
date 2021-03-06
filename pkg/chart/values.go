package chart

import (
	"errors"
	"io"
	"io/ioutil"
	"strings"

	"github.com/BurntSushi/toml"
)

// ErrNoTable indicates that a chart does not have a matching table.
var ErrNoTable = errors.New("no table")

// Values represents a collection of chart values.
type Values map[string]interface{}

// Table gets a table (TOML subsection) from a Values object.
//
// The table is returned as a Values.
//
// Compound table names may be specified with dots:
//
//	foo.bar
//
// The above will be evaluated as "The table bar inside the table
// foo".
//
// An ErrNoTable is returned if the table does not exist.
func (v Values) Table(name string) (Values, error) {
	names := strings.Split(name, ".")
	table := v
	var err error

	for _, n := range names {
		table, err = tableLookup(table, n)
		if err != nil {
			return table, err
		}
	}
	return table, err
}

// Encode writes serialized Values information to the given io.Writer.
func (v Values) Encode(w io.Writer) error {
	return toml.NewEncoder(w).Encode(v)
}

func tableLookup(v Values, simple string) (Values, error) {
	v2, ok := v[simple]
	if !ok {
		return v, ErrNoTable
	}
	vv, ok := v2.(map[string]interface{})
	if !ok {
		return vv, ErrNoTable
	}
	return vv, nil
}

// ReadValues will parse TOML byte data into a Values.
func ReadValues(data []byte) (Values, error) {
	out := map[string]interface{}{}
	err := toml.Unmarshal(data, out)
	return out, err
}

// ReadValuesFile will parse a TOML file into a Values.
func ReadValuesFile(filename string) (Values, error) {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return map[string]interface{}{}, err
	}
	return ReadValues(data)
}
