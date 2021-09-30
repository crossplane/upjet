/*
Copyright 2021 The Crossplane Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package types

import (
	"strings"

	"github.com/fatih/camelcase"
	"github.com/iancoleman/strcase"
)

// NewNameFromSnake produces a Name, using given snake case string as source of
// truth.
func NewNameFromSnake(s string) Name {
	originals := strings.Split(s, "_")
	camels := make([]string, len(originals))
	computedCamels := make([]string, len(originals))
	for i, org := range originals {
		computedCamels[i] = strcase.ToCamel(org)
		if known, ok := lowerToCamelAcronyms[strings.ToLower(org)]; ok {
			camels[i] = known
			continue
		}
		camels[i] = computedCamels[i]
	}
	return Name{
		Snake:              s,
		Camel:              strings.Join(camels, ""),
		CamelComputed:      strings.Join(computedCamels, ""),
		LowerCamel:         strings.Join(append([]string{strings.ToLower(camels[0])}, camels[1:]...), ""),
		LowerCamelComputed: strings.Join(append([]string{strings.ToLower(computedCamels[0])}, computedCamels[1:]...), ""),
	}
}

// NewNameFromCamel produces a Name, using given camel case string as source of
// truth.
func NewNameFromCamel(s string) Name {
	originals := camelcase.Split(s)
	snakes := make([]string, len(originals))
	for i, org := range originals {
		snakes[i] = strings.ToLower(org)
	}
	return NewNameFromSnake(strings.Join(snakes, "_"))
}

// Name holds different variants of a name.
type Name struct {
	// Snake is the snake case version of the string: rds_instance
	Snake string

	// Camel is the camel case version of the string where known acronyms are
	// are uppercase: RDSInstance
	Camel string

	// LowerCamel is the camel case version with the first word being lower case
	// and the known acronyms are uppercase if they are not the first word: rdsInstance
	LowerCamel string

	// CamelComputed is the camel case version without any acronym changes: RdsInstance
	CamelComputed string

	// LowerCamelComputed is the camel case version without any acronym changes
	// and the first word is lower case: rdsInstance
	LowerCamelComputed string
}

// Add acronyms that can be safely assumed to be common for any kind of provider.
// For provider-specific ones, like ARN for Amazon Web Services, provider
// authors need to configure them in their provider repository.

// NOTE(muvaf): We can have more maps like camel -> lower for reverse conversion,
// but it's not necessary for now.

var (
	// Used to hold lower -> camel known exceptions.
	lowerToCamelAcronyms = map[string]string{
		"id": "ID",
	}
)

// AddAcronym is used to add exception words that will be used to intervene
// the conversion from lower case to camel case.
// It is suggested that users of this package make all AddAcronym calls before
// any usage (like init()) so that the conversions are consistent across the
// board.
func AddAcronym(lower, camel string) {
	lowerToCamelAcronyms[lower] = camel
}

func init() {
	// Written manually
	AddAcronym("ipv6", "IPv6")
	AddAcronym("ipv4", "IPv4")

	// Taken from golangci-lint staticcheck
	// https://github.com/dominikh/go-tools/blob/4049766cbbeee505b10996f03cd3f504aa238734/config/example.conf#L2
	AddAcronym("acl", "ACL")
	AddAcronym("api", "API")
	AddAcronym("ascii", "ASCII")
	AddAcronym("cpu", "CPU")
	AddAcronym("css", "CSS")
	AddAcronym("dns", "DNS")
	AddAcronym("eof", "EOF")
	AddAcronym("guid", "GUID")
	AddAcronym("html", "HTML")
	AddAcronym("http", "HTTP")
	AddAcronym("https", "HTTPS")
	AddAcronym("id", "ID")
	AddAcronym("ip", "IP")
	AddAcronym("json", "JSON")
	AddAcronym("qps", "QPS")
	AddAcronym("ram", "RAM")
	AddAcronym("rpc", "RPC")
	AddAcronym("sla", "SLA")
	AddAcronym("smtp", "SMTP")
	AddAcronym("sql", "SQL")
	AddAcronym("ssh", "SSH")
	AddAcronym("tcp", "TCP")
	AddAcronym("tls", "TLS")
	AddAcronym("ttl", "TTL")
	AddAcronym("udp", "UDP")
	AddAcronym("ui", "UI")
	AddAcronym("gid", "GID")
	AddAcronym("uid", "UID")
	AddAcronym("uuid", "UUID")
	AddAcronym("uri", "URI")
	AddAcronym("url", "URL")
	AddAcronym("utf8", "UTF8")
	AddAcronym("vm", "VM")
	AddAcronym("xml", "XML")
	AddAcronym("xmpp", "XMPP")
	AddAcronym("xsrf", "XSRF")
	AddAcronym("xss", "XSS")
	AddAcronym("sip", "SIP")
	AddAcronym("rtp", "RTP")
	AddAcronym("amqp", "AMQP")
	AddAcronym("db", "DB")
	AddAcronym("ts", "TS")

}
