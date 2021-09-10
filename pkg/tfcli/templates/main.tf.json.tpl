{
    "terraform": {
        "required_providers": {
            "tf-provider": {
                "source":  "{{ .Provider.Source }}",
                "version": "{{ .Provider.Version }}"
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
            "{{ .Resource.LabelName }}": {
                {{ .Resource.Body | printf "%s" }}
                {{ if .Lifecycle.PreventDestroy -}}
                    ,"lifecycle" : {
                    "prevent_destroy": true
                    }
                {{ end }}
            }
        }
    }
}
