# Using the STACKIT provider extension with Gardener as end-user

## STACKIT Workload Identity

The STACKIT provider extension deploys and configures the
[`stackit-pod-identity-webhook`](https://github.com/stackitcloud/stackit-pod-identity-webhook)
in the Shoot control plane. This enables workloads running in SKE clusters to use
STACKIT Workload Identity without storing long-lived STACKIT credentials in Pods.

The extension is responsible for making the token-injection mechanism available
to the Shoot cluster. The trust relationship between the cluster and a STACKIT
Service Account is configured separately in the STACKIT Identity Provider (IdP).


STACKIT Workload Identity uses OIDC federation to exchange a Kubernetes
ServiceAccount token for a short-lived STACKIT access token.

The flow is:

1. The Kubernetes API server issues a projected, audience-bound ServiceAccount token.
2. The `stackit-pod-identity-webhook` injects the token and the required STACKIT SDK
   environment variables into Pods using an annotated Kubernetes `ServiceAccount`.
3. The STACKIT SDK exchanges the projected token with the STACKIT IdP.
4. The IdP validates the token against the Shoot's OIDC issuer and configured
   assertions.
5. The workload receives a short-lived STACKIT access token and uses it to call
   STACKIT APIs.

This removes the need for static service account keys in workloads.

## What the STACKIT provider extension provides

The provider extension:

- deploys the `stackit-pod-identity-webhook` into the Shoot control plane;
- configures the webhook so it can mutate Pods in the Shoot cluster; and
- provides the webhook with the TLS configuration required for communication
  with the Kubernetes API server.

No manual installation of the webhook is required for SKE clusters.

The extension does **not** create the STACKIT Service Account federation or define
which Kubernetes identities may assume it. This trust relationship is configured
in the STACKIT IdP.

## Prerequisites

Before configuring a workload, you need:

- a STACKIT project;
- a STACKIT Service Account with the permissions required by the workload; and
- an SKE cluster with Workload Identity support enabled.

## Step 1: Establish a trust relationship

The STACKIT IdP must trust the OIDC issuer of the Shoot cluster.

For a cluster created through SKE, retrieve the `serviceAccountIssuer` from the
cluster status. For example:

```bash
stackit ske cluster describe -p <PROJECT_ID> <CLUSTER_NAME> -o json \
  | jq -r '.status.serviceAccountIssuer'
```

When a new cluster is created, the `serviceAccountIssuer` can be populated
asynchronously. Fetch the cluster status again after the initial provisioning
request if the field is not present.

Create a **Federated Identity Provider** for the STACKIT Service Account in the
[STACKIT Portal](https://portal.stackit.cloud/), using the returned issuer URL.

At minimum, restrict the federation with the Kubernetes `sub` claim:

```text
system:serviceaccount:<namespace>:<k8s-serviceaccount>
```

For example:

```text
system:serviceaccount:default:app
```

You should also configure the audience assertion expected by the workload. The
default STACKIT Workload Identity audience is:

```text
sts.accounts.stackit.cloud
```

See the STACKIT documentation for
[creating and managing federated identity providers](https://docs.stackit.cloud/platform/access-and-identity/service-accounts/how-tos/manage-service-account-federations/).

## Step 2: Configure the workload

Annotate the Kubernetes `ServiceAccount` with the STACKIT Service Account email.
Pods using this `ServiceAccount` are then mutated automatically by the webhook.

A minimal example:

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: app
  namespace: default
  annotations:
    workload-identity.stackit.cloud/service-account-email: "my-service-account@sa.stackit.cloud"
---
apiVersion: v1
kind: Pod
metadata:
  name: app
  namespace: default
spec:
  serviceAccountName: app
  automountServiceAccountToken: false
  containers:
    - name: app
      image: <IMAGE>
```

The `workload-identity.stackit.cloud/service-account-email` annotation is the
required setting for enabling STACKIT Workload Identity for the `ServiceAccount`.

When the Pod is created, the webhook injects:

- a projected ServiceAccount token;
- `STACKIT_FEDERATED_TOKEN_FILE`;
- `STACKIT_SERVICE_ACCOUNT_EMAIL`; and
- optional STACKIT IdP configuration variables when configured.

The token is mounted at:

```text
/var/run/secrets/stackit.cloud/serviceaccount/token
```

Applications using the STACKIT SDK can use the injected configuration
automatically.

## Configuration reference

The following `ServiceAccount` annotations are supported by the
`stackit-pod-identity-webhook`:

| Annotation | Default | Description |
| --- | --- | --- |
| `workload-identity.stackit.cloud/service-account-email` | None | STACKIT Service Account email to assume. **Required.** |
| `workload-identity.stackit.cloud/audience` | `sts.accounts.stackit.cloud` | Audience of the projected ServiceAccount token. |
| `workload-identity.stackit.cloud/service-account-token-expiration-seconds` | `600` | Lifetime of the projected ServiceAccount token. |
| `workload-identity.stackit.cloud/idp-token-expiration-seconds` | SDK default (`3600`) | Requested lifetime of the token returned by the IdP. |
| `workload-identity.stackit.cloud/idp-token-endpoint` | `https://accounts.stackit.cloud/oauth/v2/token` | Token exchange endpoint. Useful for non-default IdP environments. |

Pods or namespaces can opt out of mutation with:

```yaml
metadata:
  labels:
    workload-identity.stackit.cloud/skip-pod-identity-webhook: "true"
```

## Security considerations

Use a dedicated Kubernetes `ServiceAccount` for each external identity where
practical, and keep the federation assertion as narrow as possible.

For a `ServiceAccount` used only for Workload Identity, set:

```yaml
automountServiceAccountToken: false
```

This prevents the normal ServiceAccount token from being mounted for generic
Kubernetes API access while the webhook still provides the projected token used
for federation.

Pods running on SKE worker VMs may also be able to reach the VM metadata service
and obtain infrastructure credentials. If workloads must not access VM
credentials, restrict egress to the metadata IP (`169.254.169.254`) with a
Kubernetes `NetworkPolicy`.

## Token and signing-key rotation

Projected ServiceAccount tokens are short-lived and automatically refreshed.
The default projected-token lifetime is 10 minutes.

SKE cluster ServiceAccount signing keys can be rotated through the normal
Gardener/SKE credential rotation process. During rotation, the STACKIT IdP caches
the cluster JWKS for up to one hour, so tokens signed with a previous key may
remain accepted for a limited time after rotation. Keeping the projected token
TTL short reduces this exposure.

See the [SKE credential rotation documentation](https://docs.stackit.cloud/products/runtime/kubernetes-engine/how-tos/rotate-ske-credentials/)
and the [Gardener ServiceAccount signing-key documentation](https://gardener.cloud/docs/gardener/shoot-operations/shoot_credentials_rotation/#serviceaccount-token-signing-key)
for the operational rotation procedure.

## References

- [STACKIT Workload Identity](https://docs.stackit.cloud/products/runtime/kubernetes-engine/how-tos/workload-identity/)
- [STACKIT Service Account Federation](https://docs.stackit.cloud/platform/access-and-identity/service-accounts/how-tos/manage-service-account-federations/)
- [`stackit-pod-identity-webhook`](https://github.com/stackitcloud/stackit-pod-identity-webhook)
- [Gardener Managed Service Account Issuer](https://gardener.cloud/docs/gardener/security/shoot_serviceaccounts/#managed-service-account-issuer)
- [Kubernetes ServiceAccount token projection](https://kubernetes.io/docs/concepts/storage/projected-volumes/#serviceaccounttoken)
