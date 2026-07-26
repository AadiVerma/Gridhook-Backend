package parsers

import "fmt"

// WSDLParser drafts one tool per WSDL <operation>, with each tool's
// endpoint_mapping holding the SOAP envelope template SoapEngine needs
// (soapAction + envelopeTemplate with {{param}} placeholders built from the
// operation's input message parts). Full XML-schema-aware parsing is a
// separate, sizable piece of work — stubbed here so the Parser interface
// and the rest of the pipeline (control plane, dispatcher, SoapEngine) can
// be built and tested end-to-end against hand-authored tools first.
type WSDLParser struct{}

func (p *WSDLParser) Parse(raw []byte) (*ParseResult, error) {
	return nil, fmt.Errorf("parsers: wsdl: not yet implemented — author SOAP tools via the manual tool-mapping editor for now")
}
