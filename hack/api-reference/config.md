<p>Packages:</p>
<ul>
<li>
<a href="#stackit.provider.extensions.config.stackit.cloud%2fv1alpha1">stackit.provider.extensions.config.stackit.cloud/v1alpha1</a>
</li>
</ul>

<h2 id="stackit.provider.extensions.config.stackit.cloud/v1alpha1">stackit.provider.extensions.config.stackit.cloud/v1alpha1</h2>
<p>

</p>

<h3 id="controllerconfiguration">ControllerConfiguration
</h3>


<p>
ControllerConfiguration defines the configuration for the STACKIT provider.
</p>

<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>

<tr>
<td>
<code>clientConnection</code></br>
<em>
<a href="#clientconnectionconfiguration">ClientConnectionConfiguration</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>ClientConnection specifies the kubeconfig file and client connection<br />settings for the proxy server to use when communicating with the apiserver.</p>
</td>
</tr>
<tr>
<td>
<code>etcd</code></br>
<em>
<a href="#etcd">ETCD</a>
</em>
</td>
<td>
<p>ETCD is the etcd configuration.</p>
</td>
</tr>
<tr>
<td>
<code>healthCheckConfig</code></br>
<em>
<a href="#healthcheckconfig">HealthCheckConfig</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>HealthCheckConfig is the config for the health check controller</p>
</td>
</tr>
<tr>
<td>
<code>registryCaches</code></br>
<em>
<a href="#registrycacheconfiguration">RegistryCacheConfiguration</a> array
</em>
</td>
<td>
<em>(Optional)</em>
<p>RegistryCaches optionally configures a container registry cache(s) that will be<br />configured on every shoot machine at boot time (and reconciled while its running).</p>
</td>
</tr>
<tr>
<td>
<code>deployALBIngressController</code></br>
<em>
boolean
</em>
</td>
<td>
<p>DeployALBIngressController</p>
</td>
</tr>
<tr>
<td>
<code>customLabelDomain</code></br>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>CustomLabelDomain is the domain prefix for custom labels applied to STACKIT infrastructure resources.<br />For example, cluster labels will use "<domain>/cluster" (default: "kubernetes.io").</p>
</td>
</tr>

</tbody>
</table>


<h3 id="etcd">ETCD
</h3>


<p>
(<em>Appears on:</em><a href="#controllerconfiguration">ControllerConfiguration</a>)
</p>

<p>
ETCD is an etcd configuration.
</p>

<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>

<tr>
<td>
<code>storage</code></br>
<em>
<a href="#etcdstorage">ETCDStorage</a>
</em>
</td>
<td>
<p>ETCDStorage is the etcd storage configuration.</p>
</td>
</tr>
<tr>
<td>
<code>backup</code></br>
<em>
<a href="#etcdbackup">ETCDBackup</a>
</em>
</td>
<td>
<p>ETCDBackup is the etcd backup configuration.</p>
</td>
</tr>

</tbody>
</table>


<h3 id="etcdbackup">ETCDBackup
</h3>


<p>
(<em>Appears on:</em><a href="#etcd">ETCD</a>)
</p>

<p>
ETCDBackup is an etcd backup configuration.
</p>

<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>

<tr>
<td>
<code>schedule</code></br>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>Schedule is the etcd backup schedule.</p>
</td>
</tr>

</tbody>
</table>


<h3 id="etcdstorage">ETCDStorage
</h3>


<p>
(<em>Appears on:</em><a href="#etcd">ETCD</a>)
</p>

<p>
ETCDStorage is an etcd storage configuration.
</p>

<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>

<tr>
<td>
<code>className</code></br>
<em>
string
</em>
</td>
<td>
<em>(Optional)</em>
<p>ClassName is the name of the storage class used in etcd-main volume claims.</p>
</td>
</tr>
<tr>
<td>
<code>capacity</code></br>
<em>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.33/#quantity-resource-api">Quantity</a>
</em>
</td>
<td>
<em>(Optional)</em>
<p>Capacity is the storage capacity used in etcd-main volume claims.</p>
</td>
</tr>

</tbody>
</table>


<h3 id="registrycacheconfiguration">RegistryCacheConfiguration
</h3>


<p>
(<em>Appears on:</em><a href="#controllerconfiguration">ControllerConfiguration</a>)
</p>

<p>
RegistryCacheConfiguration configures a single registry cache.
</p>

<table>
<thead>
<tr>
<th>Field</th>
<th>Description</th>
</tr>
</thead>
<tbody>

<tr>
<td>
<code>server</code></br>
<em>
string
</em>
</td>
<td>
<p>Server is the URL of the upstream registry.</p>
</td>
</tr>
<tr>
<td>
<code>cache</code></br>
<em>
string
</em>
</td>
<td>
<p>Cache is the URL of the cache registry.</p>
</td>
</tr>
<tr>
<td>
<code>caBundle</code></br>
<em>
integer array
</em>
</td>
<td>
<p>CABundle optionally specifies a CA Bundle to trust when connecting to the cache registry.</p>
</td>
</tr>
<tr>
<td>
<code>capabilities</code></br>
<em>
string array
</em>
</td>
<td>
<p>Capabilities optionally specifies what operations the cache registry is capable of.</p>
</td>
</tr>

</tbody>
</table>


