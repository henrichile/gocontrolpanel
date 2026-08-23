package mailer

import (
	"bytes"
	"html/template"
)

// WelcomeData son las variables disponibles en la plantilla "bienvenida_cliente".
type WelcomeData struct {
	FullName string
	Username string
	Password string
	PanelURL string
	Domain   string
}

// RenderTemplate interpola subjectTpl/bodyTpl con data usando html/template:
// FullName es texto libre provisto por quien crea la cuenta, así que hay que
// escaparlo para que no pueda inyectar HTML en el correo resultante.
func RenderTemplate(subjectTpl, bodyTpl string, data any) (subject, body string, err error) {
	subjectOut, err := renderOne("subject", subjectTpl, data)
	if err != nil {
		return "", "", err
	}
	bodyOut, err := renderOne("body", bodyTpl, data)
	if err != nil {
		return "", "", err
	}
	return subjectOut, bodyOut, nil
}

func renderOne(name, tpl string, data any) (string, error) {
	t, err := template.New(name).Parse(tpl)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}
