{
    "terraform": {
        "required_providers": {
            "tf-provider": {
                "source":  "{{ .ProviderSource }}",
                "version": "{{ .ProviderVersion }}"
            }
        }
    },

    {{ if .ProviderConfiguration -}}
    "provider": {
        "tf-provider": [
            {{ .ProviderConfiguration | printf "%s" }}
        ]
    },
    {{ end }}

    "resource": {
        "{{ .ResourceType }}": {
            "{{ .ResourceName }}": {{ .ResourceBody | printf "%s" }}
        }
    }
}
