package config

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
)

type stringTemplate struct {
	templateText string
	template     *template.Template
}

func (f *stringTemplate) Decode(value string) error {
	temp, err := template.New("").Parse(strings.Replace(value, "{{", "{{.", -1))

	if err != nil {
		return fmt.Errorf("Can't parse format: %s", err)
	}

	*f = stringTemplate{
		templateText: value,
		template:     temp,
	}

	return nil
}

func (f *stringTemplate) Parse(a ...string) string {
	var buf bytes.Buffer

	m := make(map[string]string, len(a)/2)
	for i := 0; i < len(a); i += 2 {
		m[a[i]] = a[i+1]
	}

	f.template.Execute(&buf, m)

	return buf.String()
}

func (f stringTemplate) String() string {
	return f.templateText
}

func (f stringTemplate) MarshalJSON() ([]byte, error) {
	return []byte("\"" + f.String() + "\""), nil
}
