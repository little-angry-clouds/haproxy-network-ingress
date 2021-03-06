# Haproxy Network Ingress Controller
This is a work in progress, there's not even a first release. Use under your risk.

[![Go Report Card](https://goreportcard.com/badge/github.com/little-angry-clouds/haproxy-network-ingress)](https://goreportcard.com/report/github.com/little-angry-clouds/haproxy-network-ingress)
[![GitHub license](https://img.shields.io/github/license/little-angry-clouds/haproxy-network-ingress.svg)](https://github.com/little-angry-clouds/haproxy-network-ingress/blob/master/LICENSE)

[![Asciinema](https://asciinema.org/a/282418.svg)](https://asciinema.org/a/282418?autoplay=1)

## Description
This repository contains the Haproxy controller build around the Kubernetes
Network Ingress resource that uses ConfigMap to store the Haproxy configuration.

## What is a Network Ingress?
It's a resource very similar to the ingress in the way
that exposes kubernetes aplications (it can export arbitrary things, thought),
but if the ingress exposes them as a HTTP proxy, the Network Ingress exposes
them as a TCP proxy. You may want to see it's [design document](./docs/network-ingress-design.md).

Why would you want a TCP proxy in Kubernetes, you may ask? There's at least two
use cases (if you find more, please open an issue!).

### Use case 1
The first one is expose remote resources to a developer. An usual
workflow is working with a k8s cluster in a cloud like AWS. There's a lot of
work on getting stateful applications like databases on kubernetes, but it's
too soon to say that it's trustworthy. Also, even if they were, since you're on
a public cloud, you may want to trust AWS to run your databases. If you do so,
you will follow, of course, ![their security
recomendations](https://docs.aws.amazon.com/vpc/latest/userguide/VPC_Scenario2.html),
which say that you should have your RDS in private subnets. This way, even if
you open your Security Groups, you won't be able to reach the RDS. But you need
to! At least in prior environments, you should be able to connect to your
database to do database stuff. You have some options. You could connect through
a SSH bastion machine, which AWS recommends. You could also connect through a
VPN. But why do that, if you have a kubernetes cluster? You can configure a TCP
proxy an have kubernetes handle it for you!

An example workflow would be the next. We assume you have the Haproxy Network
Ingress installed in your cluster. You would need to create the next resource to
expose an example RDS:

``` yaml
apiVersion: little-angry-clouds.k8s.io/v1
kind: NetworkIngress
metadata:
  name: my-cool-rds
  namespace: default
spec:
  rules:
    - host: my-cool-rds.randomString.eu-west-1.rds.amazonaws.com
      port: 3306
      targetPort: 3306
      name: my-cool-rds
```

When you create the previous resource, the controller will create the
configuration for the Haproxy proxy and create a new service associated to the
3306 port on the Haproxy deployment. This way, you'll only have to forward your
port with:

``` yaml
kubectl port-forward service/my-cool-rds 3306:3306
```

And you will access to your remote RDS securely and easily as it were in your
localhost!

### Use case 2
This use case is in the ![TODO
list](https://github.com/little-angry-clouds/haproxy-network-ingress/issues/15).

This one is the exact opposite as the previous one. In the previous use case
we wanted to access remote resources through kubernetes. In this one we want to
access to resources on kubernetes from remote resources.

Imagine the same environment as the previous use case, a kubernetes cluster in
AWS. But this time, you're greedy! You decide that the ![MySQL
Operator](https://github.com/oracle/mysql-operator) and the ![Kafka
Operator](https://github.com/banzaicloud/kafka-operator) are good enought for
your development environment. Also you have some lambdas that need to access
both of your services (for whatever reason).

Without the Network Ingress, your only wat to expose both resources would be to
create two Load Balancer. That's ok, but rememeber, you're greedy! Why use one
per service when you can use one?! Also, this tends to infinity. As you trust
more in the operators, you may want to have more services that you're now using
on AWS. So having just one LB sounds cool. This way the only LB will point to
the controller's backend deployment, which will redirect to every exposed
service.

To do it, you would create the next Network Ingress:

``` yaml
apiVersion: little-angry-clouds.k8s.io/v1
kind: NetworkIngress
metadata:
  name: my-cool-mysql
  namespace: default
  labels:
    # The default type is internal
    kubernetes.io/network-ingress.type: external
spec:
  rules:
    - host: my-cool-mysql
      port: 3306
      targetPort: 3306
      name: my-cool-rds
    - host: my-cool-kafka
      port: 9092
      targetPort: 9092
      name: my-cool-kafka
    - host: my-cool-zookeeper
      port: 2181
      targetPort: 2181
      name: my-cool-zookeeper
```

This way, you'll have one LB with multiple ports pointing to your precious services.

## Install

The recommended way to use this controller is using it's [helm
chart](https://github.com/little-angry-clouds/charts/tree/master/haproxy-network-ingress).

But if you want to use the standalone binary, you may do it. You can download
the compiled binary in the [releases
page](https://github.com/little-angry-clouds/haproxy-network-ingress/releases/).

## Issues
You may open an issue with any problem or question you have.

## Tests

There's actually some integration tests that you can see in the `tests`
directory. They're done with  [[https://github.com/bats-core/bats-core][bats]],
a tool whoose name stands for "Bash Automated Testing System". It uses kind to
create a temporary cluster, deploys it and executes the tests. It deletes it
when the tests are done.

To speed up things when developing, the first time you execute you may export
the variable `BATS_CLEAN_TEST=false`, which will make the cluster to still be
available when finishing the tests. Then, in the next execution, you get the
test ID and and export it to the variable `BATS_TEST_ID=$test`. For example:

``` bash
> time BATS_CLEAN_TEST=false bats tests
  Test cluster with ID: U4UFsZ4 ...
 ✓ verify that the controller creates correctly the services
 ✓ verify that the controller creates correctly the deployment's ports
 ✓ verify the uninstallation

4 tests, 0 failures

BATS_CLEAN_TEST=false bats tests  15,02s user 5,90s system 14% cpu 2:24,18 total
```

And without re-creating the cluster:

``` bash
> time BATS_TEST_ID=U4UFsZ4 bats tests
 ✓ verify the components of the deployment
 ✓ verify that the controller creates correctly the services
 ✓ verify that the controller creates correctly the deployment's ports
 ✓ verify the uninstallation

4 tests, 0 failures
BATS_TEST_ID=LyLHV0K bats tests  9,50s user 1,79s system 20% cpu 56,298 total
```

## Thanks
As you may have noticed, this README it's very similar to the one on the ![NGINX
Ingress repository](https://github.com/kubernetes/ingress-nginx), so thanks to
them to doing such a great README.

![The kubebuilder
book](https://kubebuilder.io/cronjob-tutorial/cronjob-tutorial.html) is awesome!
It makes very easy developing a Controller even if you don't know what you're doing.

## License
![GPL3](LICENSE)
