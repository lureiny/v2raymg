package template

import (
	"bytes"
	osTemplate "text/template"
)

func RenderHysteriaConfigTemplate(templateVars map[string]interface{}) (string, error) {
	return renderTemplate("hysteria", HysteriaConfigTemplate, templateVars)
}

func RenderXrayOrV2rayConfigTemplate(templateVars map[string]interface{}) (string, error) {
	return renderTemplate("xray/v2ray", XrayOrV2rayConfigTemplate, templateVars)
}

func renderTemplate(name, templateString string, templateVars map[string]interface{}) (string, error) {
	render, err := osTemplate.New(name).Parse(templateString)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := render.Execute(&buf, templateVars); err != nil {
		return "", err
	}
	return buf.String(), nil
}
