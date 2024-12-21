package junit

import (
	"encoding/xml"
	"fmt"
	"io"
)

// WriteXML writes the XML representation of Testsuites t to writer w.
func (t *Testsuite) WriteXML(w io.Writer) error {
	enc := xml.NewEncoder(w)
	enc.Indent("", "\t")
	if err := enc.Encode(t); err != nil {
		return err
	}
	if err := enc.Flush(); err != nil {
		return err
	}
	_, err := fmt.Fprintf(w, "\n")
	return err
}

// Testsuite is a single JUnit testsuite containing testcases.
type Testsuite struct {
	XMLName xml.Name `xml:"testsuite"` //what magic is this
	// required attributes
	Name     string `xml:"name,attr"`
	Tests    int    `xml:"tests,attr"`
	Failures int    `xml:"failures,attr"`

	Testcases []Testcase `xml:"testcase,omitempty"`
}

// AddTestcase adds Testcase tc to this Testsuite.
func (t *Testsuite) AddTestcase(tc Testcase) {
	t.Testcases = append(t.Testcases, tc)
	t.Tests++
	if tc.Failure != nil {
		t.Failures++
	}

}

// Testcase represents a single test with its results.
type Testcase struct {
	// required attributes
	Name      string `xml:"name,attr"`
	Classname string `xml:"classname,attr"`

	Failure *Result `xml:"failure,omitempty"`
}

// Property represents a key/value pair.
type Property struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

// Result represents the result of a single test.
type Result struct {
	Message string `xml:"message,attr"`
	Type    string `xml:"type,attr,omitempty"`
}
