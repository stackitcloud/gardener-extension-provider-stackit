# Integration Tests

The infraflow code path of the infrastructure controller contains nearly no unit tests, as it primarily consists
of code interacting with the STACKIT IaaS API. Instead, these code paths are tested via the `infrastructure` integration
tests.

To run them, set the environment variables below and run `make test-integration-infra`.

```bash
export STACKIT_PROJECT_ID=<PROJECT_ID>
export STACKIT_SERVICE_ACCOUNT_KEY=$(pbpaste)
make test-integration-infra
```

The `STACKIT_SERVICE_ACCOUNT_KEY` is simply the JSON struct obtained from the Portal or API, when creating a new Service-Account key.
Additionally the ServiceAccount also needs to have the `iaas.network.admin` as well as the `iaas.isolated-network.admin` roles in-order to
create all necessary resources via the API.
