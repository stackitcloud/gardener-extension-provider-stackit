# Using the STACKIT provider extension with Gardener as end-user

## STACKIT Workload Identity with Gardener

This extension automatically deploys and configures the `stackit-pod-identity-webhook` in your SKE cluster's control plane, enabling workloads to use STACKIT Workload Identity.

### What this extension provides

- Automatic deployment of the `stackit-pod-identity-webhook` in the Shoot control plane
- Webhook configuration to inject STACKIT authentication into Pods
- TLS setup for webhook communication with the Kubernetes API server

No manual webhook installation is required — the extension handles this for you.

### Quick start

**Prerequisites:**

- An SKE cluster with Workload Identity support enabled
- A STACKIT Service Account with appropriate permissions
- A federated identity configured in the STACKIT IdP

#### Create a federated identity in STACKIT

Retrieve your cluster's OIDC issuer:

```bash
stackit ske cluster describe -p <PROJECT_ID> <CLUSTER_NAME> -o json \
  | jq -r '.status.serviceAccountIssuer'
```

Create a Federated Identity Provider in the STACKIT Portal using this issuer URL. Restrict the federation with the Kubernetes `sub` claim: `system:serviceaccount:<namespace>:<service-account-name>`

#### Annotate your Kubernetes `ServiceAccount`

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: app
  namespace: default
  annotations:
    workload-identity.stackit.cloud/service-account-email: "my-service-account@sa.stackit.cloud"
```

Pods using this `ServiceAccount` will automatically have STACKIT authentication injected.

### Supported annotations

| Annotation | Default | Description |
| --- | --- | --- |
| `workload-identity.stackit.cloud/service-account-email` | None | STACKIT Service Account email to assume. **Required.** |
| `workload-identity.stackit.cloud/audience` | `sts.accounts.stackit.cloud` | Audience for the token. |
| `workload-identity.stackit.cloud/service-account-token-expiration-seconds` | `600` | Token lifetime in seconds. |
| `workload-identity.stackit.cloud/idp-token-endpoint` | `https://accounts.stackit.cloud/oauth/v2/token` | Token exchange endpoint. |

Opt out of mutation with: `workload-identity.stackit.cloud/skip-pod-identity-webhook: "true"` label on Pod or Namespace.

### Learn more

For detailed information about STACKIT Workload Identity, see the [STACKIT documentation](https://docs.stackit.cloud/products/runtime/kubernetes-engine/how-tos/workload-identity/).

### References

- [STACKIT Service Account Federation](https://docs.stackit.cloud/platform/access-and-identity/service-accounts/how-tos/manage-service-account-federations/)
- [`stackit-pod-identity-webhook`](https://github.com/stackitcloud/stackit-pod-identity-webhook)
- [Gardener Managed Service Account Issuer](https://gardener.cloud/docs/gardener/security/shoot_serviceaccounts/#managed-service-account-issuer)
- [Kubernetes ServiceAccount token projection](https://kubernetes.io/docs/concepts/storage/projected-volumes/#serviceaccounttoken)


## STACKIT Application Load Balancer

This extension automatically deploys and configures the STACKIT Application Load Balancer (ALB) Controller in your SKE cluster's control plane, enabling workloads to create Application Load Balancers using Kubernetes `Ingress` resources.

### Quick start

Refer to [usage doc](https://github.com/stackitcloud/application-load-balancer-controller/blob/main/docs/user.md) of Application Load Balancer for setup. 

### Enable the extension

The Application Load Balancer extension is enabled through the SKE API:

```yaml
extensions:
  applicationLoadBalancer:
    enabled: true
  	ingress:
	  enabled: true
```

NOTE: Ingress support needs to be enabled in order to use Application Load Balancer extension.

### Learn more

For more information about the STACKIT Application Load Balancer Controller, see the [`application-load-balancer-controller`](https://github.com/stackitcloud/application-load-balancer-controller) repository.

# Migrate from Openstack to STACKIT provider

We still have openstack code runnnig in the repository, and in future it will be fully migrated and use stackit provider.
