// Package env provides a few helpers to load in environment variables
// with defaults
package env

import (
	"fmt"
	"io/ioutil"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/circleci/ex/config/secret"

	multierror "github.com/hashicorp/go-multierror"
)

type Var struct {
	env     string
	envType string
	def     interface{}
}

func (f Var) String() string {
	return fmt.Sprintf("%-40s %-12s (%v)", f.env, f.envType, f.def)
}

func (f Var) Name() string {
	return f.env
}

type Loader struct {
	vars map[string]Var // a map of all the vars this loader has been asked to load
	err  error
}

func NewLoader() *Loader {
	return &Loader{
		vars: make(map[string]Var),
	}
}

func (l *Loader) Err() error {
	return l.err
}

// SecretFromFile loads in the content of the file given in the env var.
// A slight potential trap is that the default value provided would be
// the content of the file and not the file path.
// If the env var is not set or is set but is empty then the default
// value is left unaltered and the loader multi error added to.
func (l *Loader) SecretFromFile(fld *secret.String, env string) {
	l.addVar(*fld, env, "file")
	fn, ok := os.LookupEnv(env)
	if !ok || fn == "" {
		return
	}
	content, err := ioutil.ReadFile(fn) // #nosec G304 - we know we are reading secrets from files
	if err != nil {
		l.err = multierror.Append(l.err, fmt.Errorf("failed to read secret file: %w", err))
		return
	}
	*fld = secret.String(content)
}

// String inspects the system env var given by env. If it is present it will
// set the contents of fld.
func (l *Loader) String(fld *string, env string) {
	l.addVar(*fld, env, "string")
	val, ok := os.LookupEnv(env)
	if !ok {
		return
	}
	*fld = val
}

// Int inspects the system env var given by env. If it is present
// it will parse the value as an int as per Atoi to set the contents of fld.
// If the parse fails the content of fld is left unaltered and the
// loader multi error added to.
func (l *Loader) Int(fld *int, env string) {
	l.addVar(*fld, env, "int")
	val, ok := os.LookupEnv(env)
	if !ok {
		return
	}
	i, err := strconv.Atoi(val)
	if err != nil {
		l.err = multierror.Append(l.err, fmt.Errorf("env var: %q caused an error: %w", env, err))
		return
	}
	*fld = i
}

// Bool inspects the system env var given by env. If it is present
// it will use the truthy or falsy strings as per ParseBool to set
// the contents of fld.
// If the parse fails the content of fld is left unaltered and the
// loader multi error added to.
func (l *Loader) Bool(fld *bool, env string) {
	l.addVar(*fld, env, "bool")
	val, ok := os.LookupEnv(env)
	if !ok {
		return
	}
	b, err := strconv.ParseBool(val)
	if err != nil {
		l.err = multierror.Append(l.err, fmt.Errorf("env var: %q caused an error: %w", env, err))
		return
	}
	*fld = b
}

type Vars []Var

// Sort the vars v in place alphabetically
func (v Vars) Sort() {
	sort.Slice(v, func(i, j int) bool {
		return v[i].env < v[j].env
	})
}

// SortUnique makes sure the names of the Vars in v are unique removing duplicates
// and sorts the resulting vars v in place alphabetically. The receiver v may shrink
// as a result.
func (v *Vars) SortUnique() {
	m := map[string]Var{}
	for _, e := range *v {
		m[e.Name()] = e
	}
	// since the new slice will always be smaller or the same size
	// we can avoid an allocation and update in place
	*v = (*v)[:len(m)]
	i := 0
	for _, vv := range m {
		(*v)[i] = vv
		i++
	}
	v.Sort()
}

func (l *Loader) VarsUsed() Vars {
	vars := make(Vars, 0, len(l.vars))
	const maxDefaultLen = 80
	for _, v := range l.vars {
		if def, ok := v.def.(string); ok {
			def = strings.Replace(def, "\n", "\\n", -1)
			if len(def) > maxDefaultLen {
				def = def[:maxDefaultLen] + " ..."
			}
			v.def = def
		}
		vars = append(vars, v)
	}
	vars.Sort()
	return vars
}

func (l *Loader) ChangeDefault(env, def string) {
	prev, ok := l.vars[env]
	if !ok {
		return
	}
	prev.def = def
	l.vars[env] = prev
}

func (l *Loader) addVar(def interface{}, env, envType string) {
	if _, ok := l.vars[env]; ok {
		panic("duplicate environment variable " + env)
	}
	l.vars[env] = Var{
		env:     env,
		envType: envType,
		def:     def,
	}
}
