package parsers

import (
	"encoding/xml"
	"fmt"
	"strings"

	"gridhook.dev/connector-backend/internal/models"
)

type WSDLParser struct{}

func (p *WSDLParser) Parse(raw []byte) (*ParseResult, error) {
	var def wsdlDefinitions
	if err := xml.Unmarshal(raw, &def); err != nil {
		return nil, fmt.Errorf("parsers: wsdl: invalid document: %w", err)
	}
	if len(def.PortTypes) == 0 {
		return nil, fmt.Errorf("parsers: wsdl: no portType found")
	}

	targetNS := def.TargetNamespace
	elements := map[string]wsdlXSDElement{}
	complexTypes := map[string]wsdlXSDComplexType{}
	for _, schema := range def.Types.Schemas {
		ns := schema.TargetNamespace
		if ns == "" {
			ns = targetNS
		}
		for _, el := range schema.Elements {
			elements[el.Name] = el
		}
		for _, ct := range schema.ComplexTypes {
			complexTypes[ct.Name] = ct
		}
		if ns != "" {
			targetNS = ns
		}
	}

	messages := map[string]wsdlMessage{}
	for _, m := range def.Messages {
		messages[m.Name] = m
	}

	soapActions := map[string]string{}
	for _, b := range def.Bindings {
		for _, op := range b.Operations {
			soapActions[op.Name] = op.SoapOperation.SoapAction
		}
	}

	var baseURL string
	for _, svc := range def.Services {
		for _, port := range svc.Ports {
			if port.Address.Location != "" {
				baseURL = port.Address.Location
			}
		}
	}

	result := &ParseResult{EngineType: models.EngineSOAP, BaseURL: baseURL}
	for _, pt := range def.PortTypes {
		for _, op := range pt.Operations {
			if op.Input == nil {
				continue
			}
			fields, err := resolveMessageFields(op.Input.Message, messages, elements, complexTypes)
			if err != nil {
				return nil, fmt.Errorf("parsers: wsdl: operation %q: %w", op.Name, err)
			}

			result.Tools = append(result.Tools, DraftTool{
				Name:        op.Name,
				Method:      models.MethodPOST,
				Description: strings.TrimSpace(op.Documentation),
				Parameters:  fieldsToJSONSchema(fields),
				EndpointMapping: map[string]any{
					"envelopeTemplate": buildEnvelopeTemplate(targetNS, fields.elementName, fields.list),
					"soapAction":       soapActions[op.Name],
					"targetNamespace":  targetNS,
					"elementName":      fields.elementName,
					"fields":           fieldsToStructured(fields.list),
				},
			})
		}
	}

	return result, nil
}

type wsdlDefinitions struct {
	TargetNamespace string         `xml:"targetNamespace,attr"`
	Types           wsdlTypes      `xml:"types"`
	Messages        []wsdlMessage  `xml:"message"`
	PortTypes       []wsdlPortType `xml:"portType"`
	Bindings        []wsdlBinding  `xml:"binding"`
	Services        []wsdlService  `xml:"service"`
}

type wsdlTypes struct {
	Schemas []wsdlXSDSchema `xml:"schema"`
}

type wsdlXSDSchema struct {
	TargetNamespace string               `xml:"targetNamespace,attr"`
	Elements        []wsdlXSDElement     `xml:"element"`
	ComplexTypes    []wsdlXSDComplexType `xml:"complexType"`
}

type wsdlXSDElement struct {
	Name        string              `xml:"name,attr"`
	Type        string              `xml:"type,attr"`
	MinOccurs   string              `xml:"minOccurs,attr"`
	MaxOccurs   string              `xml:"maxOccurs,attr"`
	ComplexType *wsdlXSDComplexType `xml:"complexType"`
}

type wsdlXSDComplexType struct {
	Name     string          `xml:"name,attr"`
	Sequence wsdlXSDSequence `xml:"sequence"`
}

type wsdlXSDSequence struct {
	Elements []wsdlXSDElement `xml:"element"`
}

type wsdlMessage struct {
	Name  string     `xml:"name,attr"`
	Parts []wsdlPart `xml:"part"`
}

type wsdlPart struct {
	Name    string `xml:"name,attr"`
	Element string `xml:"element,attr"`
	Type    string `xml:"type,attr"`
}

type wsdlPortType struct {
	Operations []wsdlOperation `xml:"operation"`
}

type wsdlOperation struct {
	Name          string    `xml:"name,attr"`
	Documentation string    `xml:"documentation"`
	Input         *wsdlOpIO `xml:"input"`
	Output        *wsdlOpIO `xml:"output"`
}

type wsdlOpIO struct {
	Message string `xml:"message,attr"`
}

type wsdlBinding struct {
	Operations []wsdlBindingOperation `xml:"operation"`
}

type wsdlBindingOperation struct {
	Name          string `xml:"name,attr"`
	SoapOperation struct {
		SoapAction string `xml:"soapAction,attr"`
	} `xml:"operation"`
}

type wsdlService struct {
	Ports []wsdlPort `xml:"port"`
}

type wsdlPort struct {
	Address struct {
		Location string `xml:"location,attr"`
	} `xml:"address"`
}

type field struct {
	name     string
	xsdType  string
	repeated bool
}

type resolvedFields struct {
	elementName string
	list        []field
}

func resolveMessageFields(messageRef string, messages map[string]wsdlMessage, elements map[string]wsdlXSDElement, complexTypes map[string]wsdlXSDComplexType) (resolvedFields, error) {
	msg, ok := messages[stripPrefix(messageRef)]
	if !ok {
		return resolvedFields{}, fmt.Errorf("message %q not found", messageRef)
	}
	if len(msg.Parts) == 0 {
		return resolvedFields{}, nil
	}
	part := msg.Parts[0]

	if part.Element != "" {
		el, ok := elements[stripPrefix(part.Element)]
		if !ok {
			return resolvedFields{}, fmt.Errorf("element %q not found", part.Element)
		}
		ct := el.ComplexType
		if ct == nil {
			if named, ok := complexTypes[stripPrefix(el.Type)]; ok {
				ct = &named
			}
		}
		if ct == nil {
			return resolvedFields{elementName: el.Name}, nil
		}
		fields := make([]field, 0, len(ct.Sequence.Elements))
		for _, child := range ct.Sequence.Elements {
			fields = append(fields, field{
				name:     child.Name,
				xsdType:  stripPrefix(child.Type),
				repeated: child.MaxOccurs == "unbounded",
			})
		}
		return resolvedFields{elementName: el.Name, list: fields}, nil
	}

	fields := make([]field, 0, len(msg.Parts))
	for _, p := range msg.Parts {
		fields = append(fields, field{name: p.Name, xsdType: stripPrefix(p.Type)})
	}
	return resolvedFields{list: fields}, nil
}

func stripPrefix(qname string) string {
	if i := strings.LastIndex(qname, ":"); i >= 0 {
		return qname[i+1:]
	}
	return qname
}

func xsdTypeToJSONSchema(xsdType string) map[string]any {
	switch xsdType {
	case "int", "integer", "long", "short", "unsignedInt", "unsignedLong", "unsignedShort":
		return map[string]any{"type": "integer"}
	case "decimal", "float", "double":
		return map[string]any{"type": "number"}
	case "boolean":
		return map[string]any{"type": "boolean"}
	case "":
		return map[string]any{"type": "object"}
	default:
		return map[string]any{"type": "string"}
	}
}

func fieldsToJSONSchema(f resolvedFields) map[string]any {
	properties := map[string]any{}
	required := make([]any, 0, len(f.list))
	for _, fl := range f.list {
		schema := xsdTypeToJSONSchema(fl.xsdType)
		if fl.repeated {
			schema = map[string]any{"type": "array", "items": schema}
		}
		properties[fl.name] = schema
		required = append(required, fl.name)
	}
	return map[string]any{
		"type":       "object",
		"properties": properties,
		"required":   required,
	}
}

func fieldsToStructured(fields []field) []any {
	out := make([]any, 0, len(fields))
	for _, f := range fields {
		out = append(out, map[string]any{
			"name":     f.name,
			"xsdType":  f.xsdType,
			"jsonType": xsdTypeToJSONSchema(f.xsdType)["type"],
			"repeated": f.repeated,
		})
	}
	return out
}

func buildEnvelopeTemplate(targetNS, elementName string, fields []field) string {
	var body strings.Builder
	if elementName != "" {
		fmt.Fprintf(&body, `<tns:%s>`, elementName)
		for _, f := range fields {
			fmt.Fprintf(&body, `<%s>{{%s}}</%s>`, f.name, f.name, f.name)
		}
		fmt.Fprintf(&body, `</tns:%s>`, elementName)
	} else {
		for _, f := range fields {
			fmt.Fprintf(&body, `<%s>{{%s}}</%s>`, f.name, f.name, f.name)
		}
	}

	return fmt.Sprintf(
		`<?xml version="1.0" encoding="utf-8"?>`+
			`<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/" xmlns:tns="%s">`+
			`<soap:Body>%s</soap:Body>`+
			`</soap:Envelope>`,
		targetNS, body.String(),
	)
}
