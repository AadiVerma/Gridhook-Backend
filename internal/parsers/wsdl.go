package parsers

import "fmt"

type WSDLParser struct{}

func (p *WSDLParser) Parse(raw []byte) (*ParseResult, error) {
	return nil, fmt.Errorf("parsers: wsdl: not yet implemented — author SOAP tools via the manual tool-mapping editor for now")
}
