package parsers

import (
	"strings"
	"testing"

	"gridhook.dev/connector-backend/internal/models"
)

const sampleWSDL = `<?xml version="1.0"?>
<definitions name="WeatherService"
  targetNamespace="http://example.com/weather"
  xmlns:tns="http://example.com/weather"
  xmlns:xsd="http://www.w3.org/2001/XMLSchema"
  xmlns:soap="http://schemas.xmlsoap.org/wsdl/soap/"
  xmlns="http://schemas.xmlsoap.org/wsdl/">
  <types>
    <xsd:schema targetNamespace="http://example.com/weather">
      <xsd:element name="GetWeatherRequest">
        <xsd:complexType>
          <xsd:sequence>
            <xsd:element name="city" type="xsd:string"/>
            <xsd:element name="days" type="xsd:int"/>
          </xsd:sequence>
        </xsd:complexType>
      </xsd:element>
      <xsd:element name="GetWeatherResponse">
        <xsd:complexType>
          <xsd:sequence>
            <xsd:element name="forecast" type="xsd:string"/>
          </xsd:sequence>
        </xsd:complexType>
      </xsd:element>
    </xsd:schema>
  </types>
  <message name="GetWeatherRequestMessage">
    <part name="parameters" element="tns:GetWeatherRequest"/>
  </message>
  <message name="GetWeatherResponseMessage">
    <part name="parameters" element="tns:GetWeatherResponse"/>
  </message>
  <portType name="WeatherPortType">
    <operation name="GetWeather">
      <documentation>Get the weather forecast for a city.</documentation>
      <input message="tns:GetWeatherRequestMessage"/>
      <output message="tns:GetWeatherResponseMessage"/>
    </operation>
  </portType>
  <binding name="WeatherBinding" type="tns:WeatherPortType">
    <soap:binding style="document" transport="http://schemas.xmlsoap.org/soap/http"/>
    <operation name="GetWeather">
      <soap:operation soapAction="http://example.com/weather/GetWeather"/>
      <input><soap:body use="literal"/></input>
      <output><soap:body use="literal"/></output>
    </operation>
  </binding>
  <service name="WeatherService">
    <port name="WeatherPort" binding="tns:WeatherBinding">
      <soap:address location="http://example.com/weather/soap"/>
    </port>
  </service>
</definitions>`

func TestWSDLParser_Parse(t *testing.T) {
	result, err := (&WSDLParser{}).Parse([]byte(sampleWSDL))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if result.EngineType != models.EngineSOAP {
		t.Errorf("EngineType = %q, want SOAP", result.EngineType)
	}
	if result.BaseURL != "http://example.com/weather/soap" {
		t.Errorf("BaseURL = %q, want the service port address", result.BaseURL)
	}
	if len(result.Tools) != 1 {
		t.Fatalf("got %d tools, want 1", len(result.Tools))
	}

	tool := result.Tools[0]
	if tool.Name != "GetWeather" {
		t.Errorf("Name = %q, want GetWeather", tool.Name)
	}
	if tool.Method != models.MethodPOST {
		t.Errorf("Method = %q, want POST", tool.Method)
	}
	if tool.Description != "Get the weather forecast for a city." {
		t.Errorf("Description = %q", tool.Description)
	}

	props, _ := tool.Parameters["properties"].(map[string]any)
	if props == nil {
		t.Fatalf("Parameters.properties missing: %#v", tool.Parameters)
	}
	city, _ := props["city"].(map[string]any)
	if city["type"] != "string" {
		t.Errorf("city type = %v, want string", city["type"])
	}
	days, _ := props["days"].(map[string]any)
	if days["type"] != "integer" {
		t.Errorf("days type = %v, want integer", days["type"])
	}
	required, _ := tool.Parameters["required"].([]any)
	if len(required) != 2 {
		t.Errorf("required = %v, want [city days]", required)
	}

	soapAction, _ := tool.EndpointMapping["soapAction"].(string)
	if soapAction != "http://example.com/weather/GetWeather" {
		t.Errorf("soapAction = %q", soapAction)
	}
	envelope, _ := tool.EndpointMapping["envelopeTemplate"].(string)
	if !strings.Contains(envelope, "{{city}}") || !strings.Contains(envelope, "{{days}}") {
		t.Errorf("envelopeTemplate missing placeholders: %s", envelope)
	}
	if !strings.Contains(envelope, "tns:GetWeatherRequest") {
		t.Errorf("envelopeTemplate missing request element: %s", envelope)
	}
}

func TestWSDLParser_Parse_InvalidDocument(t *testing.T) {
	_, err := (&WSDLParser{}).Parse([]byte("not xml"))
	if err == nil {
		t.Fatal("expected error for invalid WSDL, got nil")
	}
}

func TestWSDLParser_Parse_NoPortType(t *testing.T) {
	_, err := (&WSDLParser{}).Parse([]byte(`<definitions></definitions>`))
	if err == nil {
		t.Fatal("expected error when no portType present, got nil")
	}
}
