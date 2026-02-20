# Development

## Tooling

We use the following tools for development. Some are installed via script (as stated):

- **ko** - Used to build the container images with `make images`, installed via Makefile
- **golangci-lint** - Used to lint the codebase with `make check`, installed via Makefile (Gardener Script)
- **mirrord** - Intercept and redirect traffic for local debugging

## Debug / Quick development for webhooks with mirrord

For rapid iteration cycles, you can use [`mirrord`](https://github.com/metalbear-co/mirrord/) to run or debug the
provider extension in an existing Gardener installation. Run the `make` targets `mirrord-debug` and `mirrord-run`
with your `KUBECONFIG` pointing to a seed cluster. These targets will scale down the existing deployment to 1 replica
(to intercept webhook calls), disable all controllers running in the cluster, and then run/debug the application with the files,
environment variables, and command line flags used in the cluster. All webhook requests will be intercepted and redirected to
your local machine.

When debugging, the script starts a headless `dlv` server that you can attach to from your IDE. For example, with VSCode:

```json
# launch.json
{
  "configurations": [
    {
      "name": "Debug in-cluster",
      "type": "go",
      "request": "attach",
      "mode": "remote",
      "port": 2345,
      "host": "127.0.0.1",
    }
  ]
}
```
