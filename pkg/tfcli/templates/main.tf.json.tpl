{
    "terraform": {
        "required_providers": {
            "tf-provider": {
                "source":  "{{ .Provider.Requirement.Source }}",
                "version": "{{ .Provider.Requirement.Version }}"
            }
        }
    },

    {{ if .Provider.Configuration -}}
    "provider": {
        "tf-provider": [
            {{ .Provider.Configuration | printf "%s" }}
        ]
    },
    {{ end }}

    "resource": {
        "{{ .Resource.LabelType }}": {
            "{{ .Resource.LabelName }}": {{ .Resource.Body | printf "%s" }}
        }
    }
}
