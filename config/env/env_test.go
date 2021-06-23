package env

import (
	"os"
	"testing"
	"time"

	gocmp "github.com/google/go-cmp/cmp"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
	"gotest.tools/v3/fs"

	"github.com/circleci/ex/config/secret"
)

func TestLoader_FieldsUsed(t *testing.T) {
	l := NewLoader()
	defSecret := secret.String("default-secret")
	defDuration := time.Second * 5
	defStr := "default"
	defBool := true
	defInt := 47
	l.SecretFromFile(&defSecret, "ENV_TEST_SECRET_FILE")
	l.Duration(&defDuration, "ENV_TEST_DURATION")
	l.String(&defStr, "ENV_TEST_STRING")
	defStr = `i am a long string with newlines
i am a long string with newlines
i am a long string with newlines
i am a long string with newlines
`
	l.String(&defStr, "ENV_TEST_LONG_STRING")
	l.Bool(&defBool, "ENV_TEST_BOOL")
	l.Int(&defInt, "ENV_TEST_INT")

	fs := l.VarsUsed()
	help := make([]string, len(fs))
	for i, s := range l.VarsUsed() {
		help[i] = s.String()
	}

	// N.B. Alphabetical order
	expected := []string{
		"ENV_TEST_BOOL                            bool         (true)",
		"ENV_TEST_DURATION                        Duration     (5s)",
		"ENV_TEST_INT                             int          (47)",
		"ENV_TEST_LONG_STRING                     string       " +
			`(i am a long string with newlines\ni am a long string with newlines\ni am a long  ...)`,
		"ENV_TEST_SECRET_FILE                     file         (REDACTED)",
		"ENV_TEST_STRING                          string       (default)",
	}

	assert.Check(t, cmp.DeepEqual(help, expected))
}

func TestLoader_SecretFile(t *testing.T) {
	const secretEnvVar = "ENV_TEST_SECRET"
	t.Run("good", func(t *testing.T) {
		const hideMe = "i-am-a-secret-thing"
		configFile := fs.NewFile(t, t.Name(), fs.WithContent(hideMe))
		defer configFile.Remove()
		revert := changeEnv(secretEnvVar, configFile.Path())
		defer revert()

		defaultSecret := secret.String("")
		NewLoader().SecretFromFile(&defaultSecret, secretEnvVar)
		assert.Check(t, cmp.Equal(defaultSecret.Value(), hideMe))
	})

	t.Run("empty file", func(t *testing.T) {
		configFile := fs.NewFile(t, t.Name(), fs.WithContent(""))
		defer configFile.Remove()
		revert := changeEnv(secretEnvVar, configFile.Path())
		defer revert()

		secret := secret.String("")
		NewLoader().SecretFromFile(&secret, secretEnvVar)
		assert.Check(t, cmp.Equal(secret.Value(), ""))

		// confirm default is replaced with the empty string
		secret = "foo"
		NewLoader().SecretFromFile(&secret, secretEnvVar)
		assert.Check(t, cmp.Equal(secret.Value(), ""))
	})

	t.Run("var not set", func(t *testing.T) {
		secret := secret.String("default")
		NewLoader().SecretFromFile(&secret, secretEnvVar)
		assert.Check(t, cmp.Equal(secret.Value(), "default")) // default value untouched
	})

	t.Run("var set empty", func(t *testing.T) {
		revert := changeEnv(secretEnvVar, "")
		defer revert()

		secret := secret.String("default")
		NewLoader().SecretFromFile(&secret, secretEnvVar)
		assert.Check(t, cmp.Equal(secret.Value(), "default")) // default value untouched
	})

	t.Run("file not found", func(t *testing.T) {
		revert := changeEnv(secretEnvVar, "i-really-hope-this-is-not-accidentally-a-file")
		defer revert()

		secret := secret.String("default")
		l := NewLoader()
		l.SecretFromFile(&secret, secretEnvVar)

		assert.Check(t, cmp.ErrorContains(l.Err(), "no such file"))
		// confirm default value untouched
		assert.Check(t, cmp.Equal(secret.Value(), "default"))
	})
}

func TestLoader_Duration(t *testing.T) {
	const durationEnvVar = "ENV_TEST_DURATION"
	t.Run("good", func(t *testing.T) {
		revert := changeEnv(durationEnvVar, "2h")
		defer revert()

		durationField := time.Hour * 5
		NewLoader().Duration(&durationField, durationEnvVar)
		assert.Check(t, cmp.Equal(durationField, time.Hour*2))
	})
	t.Run("env not set", func(t *testing.T) {
		durationField := time.Hour
		NewLoader().Duration(&durationField, durationEnvVar)
		assert.Check(t, cmp.Equal(durationField, time.Hour))
	})
}

func TestLoader_String(t *testing.T) {
	const stringEnvVar = "ENV_TEST_STRING"
	t.Run("good", func(t *testing.T) {
		const val = "i-am-the-string-value"
		revert := changeEnv(stringEnvVar, val)
		defer revert()

		stringField := ""
		NewLoader().String(&stringField, stringEnvVar)
		assert.Check(t, cmp.Equal(stringField, val))
	})
	t.Run("env not set", func(t *testing.T) {
		stringField := "default"
		NewLoader().String(&stringField, stringEnvVar)
		assert.Check(t, cmp.Equal(stringField, "default"))
	})
}

func TestLoader_Int(t *testing.T) {
	const intEnvVar = "ENV_TEST_INT"
	t.Run("good", func(t *testing.T) {
		const val = 48
		revert := changeEnv(intEnvVar, "48")
		defer revert()

		var intField int
		NewLoader().Int(&intField, intEnvVar)
		assert.Check(t, cmp.Equal(intField, val))
	})

	t.Run("env not set", func(t *testing.T) {
		intField := 55
		NewLoader().Int(&intField, intEnvVar)
		assert.Check(t, cmp.Equal(intField, 55))
	})

	t.Run("not an int", func(t *testing.T) {
		revert := changeEnv(intEnvVar, "forty-eight")
		defer revert()

		l := NewLoader()

		intField := 55
		l.Int(&intField, intEnvVar)
		assert.Check(t, cmp.ErrorContains(l.Err(), "invalid syntax"))
		// confirm the input field is untouched
		assert.Check(t, cmp.Equal(intField, 55))
	})
}

func TestLoader_Bool(t *testing.T) {
	const boolEnvVar = "ENV_TEST_BOOL"
	t.Run("good", func(t *testing.T) {
		const val = true
		revert := changeEnv(boolEnvVar, "true")
		defer revert()

		var boolField bool
		NewLoader().Bool(&boolField, boolEnvVar)
		assert.Check(t, cmp.Equal(boolField, val))
	})

	t.Run("env not set", func(t *testing.T) {
		boolField := true
		NewLoader().Bool(&boolField, boolEnvVar)
		assert.Check(t, cmp.Equal(boolField, true))
	})

	t.Run("not a bool", func(t *testing.T) {
		revert := changeEnv(boolEnvVar, "booble")
		defer revert()

		l := NewLoader()

		boolField := true
		l.Bool(&boolField, boolEnvVar)
		assert.Check(t, cmp.ErrorContains(l.Err(), "invalid syntax"))
		// confirm the input field is untouched
		assert.Check(t, cmp.Equal(boolField, true))
	})
}

func TestLoader_ChangeDefault(t *testing.T) {
	const stringEnvVar = "ENV_TEST_STRING"
	l := NewLoader()
	stringField := "default"
	l.String(&stringField, stringEnvVar)
	l.ChangeDefault(stringEnvVar, "new-default")
	assert.Check(t, cmp.Equal(l.VarsUsed()[0].def, "new-default"))

	// confirm changing a non existent thing does nothing bad
	l.ChangeDefault("NOT_A_VAR", "no-effect-default")
}

func TestLoader_Duplicate(t *testing.T) {
	const stringEnvVar = "ENV_TEST_STRING"
	l := NewLoader()
	stringField := "default"
	l.String(&stringField, stringEnvVar)
	assertPanic(t, func() {
		l.String(&stringField, stringEnvVar)
	})
}

func TestLoader_MultipleError(t *testing.T) {
	const badEnvVarInt = "ENV_TEST_BAD_INT"
	const badEnvVarBool = "ENV_TEST_BAD_BOOL"
	revert1 := changeEnv(badEnvVarInt, "forty-eight")
	defer revert1()
	revert2 := changeEnv(badEnvVarBool, "not-bool")
	defer revert2()

	l := NewLoader()
	intField := 0
	l.Int(&intField, badEnvVarInt)
	boolField := true
	l.Bool(&boolField, badEnvVarBool)

	assert.Check(t, cmp.ErrorContains(l.Err(), "2 errors occurred"))
}

func TestVars_SortUnique(t *testing.T) {
	vs := Vars{}
	v1 := Var{
		env:     "ENV1",
		envType: "string",
		def:     "default",
	}
	v2 := Var{
		env:     "ENV2",
		envType: "string",
		def:     "default",
	}
	vs = append(vs, v2)
	vs = append(vs, v1)
	vs = append(vs, v2)
	vs = append(vs, v1)

	// removes the duplicates
	vs.SortUnique()
	assert.Check(t, cmp.DeepEqual(Vars{v1, v2}, vs, gocmp.AllowUnexported(Var{})))

	// no duplicates
	vs = Vars{}
	vs = append(vs, v2)
	vs = append(vs, v1)
	vs.SortUnique()
	assert.Check(t, cmp.DeepEqual(Vars{v1, v2}, vs, gocmp.AllowUnexported(Var{})))
}

func changeEnv(env, val string) func() {
	old, isOldSet := os.LookupEnv(env)
	// errors not checked as failures here would fail the tests
	_ = os.Setenv(env, val)
	return func() {
		if !isOldSet {
			_ = os.Unsetenv(env)
			return
		}
		_ = os.Setenv(env, old)
	}
}

func assertPanic(t *testing.T, f func()) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("The code did not panic")
		}
	}()
	f()
}
