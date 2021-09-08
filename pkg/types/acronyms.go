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

import "github.com/iancoleman/strcase"

// This file adds acronyms that can be safely assumed to be common for any kind
// of provider. For provider-specific ones, like ARN for Amazon Web Services,
// provider authors need to configure them in their provider repository.

func init() {
	// Written manually
	strcase.ConfigureAcronym("IPv6", "ipv6")
	strcase.ConfigureAcronym("IPv4", "ipv4")

	// Taken from golangci-lint staticcheck
	// https://github.com/dominikh/go-tools/blob/4049766cbbeee505b10996f03cd3f504aa238734/config/example.conf#L2
	strcase.ConfigureAcronym("ACL", "acl")
	strcase.ConfigureAcronym("API", "api")
	strcase.ConfigureAcronym("ASCII", "ascii")
	strcase.ConfigureAcronym("CPU", "cpu")
	strcase.ConfigureAcronym("CSS", "css")
	strcase.ConfigureAcronym("DNS", "dns")
	strcase.ConfigureAcronym("EOF", "eof")
	strcase.ConfigureAcronym("GUID", "guid")
	strcase.ConfigureAcronym("HTML", "html")
	strcase.ConfigureAcronym("HTTP", "http")
	strcase.ConfigureAcronym("HTTPS", "https")
	strcase.ConfigureAcronym("ID", "id")
	strcase.ConfigureAcronym("IP", "ip")
	strcase.ConfigureAcronym("JSON", "json")
	strcase.ConfigureAcronym("QPS", "qps")
	strcase.ConfigureAcronym("RAM", "ram")
	strcase.ConfigureAcronym("RPC", "rpc")
	strcase.ConfigureAcronym("SLA", "sla")
	strcase.ConfigureAcronym("SMTP", "smtp")
	strcase.ConfigureAcronym("SQL", "sql")
	strcase.ConfigureAcronym("SSH", "ssh")
	strcase.ConfigureAcronym("TCP", "tcp")
	strcase.ConfigureAcronym("TLS", "tls")
	strcase.ConfigureAcronym("TTL", "ttl")
	strcase.ConfigureAcronym("UDP", "udp")
	strcase.ConfigureAcronym("UI", "ui")
	strcase.ConfigureAcronym("GID", "gid")
	strcase.ConfigureAcronym("UID", "uid")
	strcase.ConfigureAcronym("UUID", "uuid")
	strcase.ConfigureAcronym("URI", "uri")
	strcase.ConfigureAcronym("URL", "url")
	strcase.ConfigureAcronym("UTF8", "utf8")
	strcase.ConfigureAcronym("VM", "vm")
	strcase.ConfigureAcronym("XML", "xml")
	strcase.ConfigureAcronym("XMPP", "xmpp")
	strcase.ConfigureAcronym("XSRF", "xsrf")
	strcase.ConfigureAcronym("XSS", "xss")
	strcase.ConfigureAcronym("SIP", "sip")
	strcase.ConfigureAcronym("RTP", "rtp")
	strcase.ConfigureAcronym("AMQP", "amqp")
	strcase.ConfigureAcronym("DB", "db")
	strcase.ConfigureAcronym("TS", "ts")
}
