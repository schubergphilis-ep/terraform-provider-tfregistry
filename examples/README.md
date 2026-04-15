This directory contains examples that are used to generate documentation via
[terraform-plugin-docs](https://github.com/hashicorp/terraform-plugin-docs).

The following directory structure is expected:

```
examples/
├── provider/
│   └── provider.tf          # Provider configuration example (used for docs index page)
└── resources/
    └── tfregistry_module/
        ├── resource.tf       # Resource usage example
        └── import.sh         # Import example
```
