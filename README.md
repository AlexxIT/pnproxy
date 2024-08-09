# pnproxy

**pnproxy** - Plug and Proxy is a simple home proxy for managing Internet traffic.

Features:

- work on all devices in the local network without additional settings
- proxy settings for selected sites only
- ad blocking support (like AdGuard)

Types:

- DNS proxy
- Reverse proxy for HTTP and TLS (level 4 proxy)
- HTTP anonymous proxy

## Install

- Docker - [alexxit/pnproxy](https://hub.docker.com/r/alexxit/pnproxy)

## Setup

For example, you want to block ads and also forward all Twitter traffic through external proxy server.
And want it to work on all home devices without additional configuration on each device.

1. Install pnproxy on any server in your home network (ex. IP: `192.168.1.123`).
   It is important that ports 53, 80 and 443 be free on this server.
2. Create `pnproxy.yaml`
   ```yaml
   hosts:
     adblock: doubleclick.com doubleclick.net
     tunnel: twitter.com twimg.com t.co x.com
   
   dns:
     listen: ":53"
     rules:
       - name: adblock
         action: static address 127.0.0.1      # block this sites
       - name: tunnel
         action: static address 192.168.1.123  # redirect this sites to pnproxy
     default:
       action: doh provider cloudflare cache true  # resolve DNS for all other sites
   
   http:
     listen: ":80"
     rules:
       - name: tunnel
         action: redirect scheme https    # redirect this sites from HTTP to TLS module
   
   tls:
     listen: ":443"
     rules:
       - name: tunnel
         action: proxy_pass host 123.123.123.123 port 3128  # forward this sites to external HTTP proxy
   
   proxy:
     listen: ":8080"  # optionally run local HTTP proxy
   
   log:
     level: trace  # optionally increase log level (default - info)
   ```
3. Setup DNS server for your home router to `192.168.1.123`.

Optionally, instead of step 3, you can verify that everything works by configuring an HTTP proxy to `192.168.1.123:8080` on your PC or mobile device.

## Configuration

By default, the app looks for the `pnproxy.yaml` file in the current working directory.

```shell
pnproxy -config /config/pnproxy.yaml
```

By default all modules disabled and don't listen any ports.

## Module: Hosts

Store lists of site domains for use in other modules.

- Name comparison includes all subdomains, you don't need to specify them separately!
- Names can be written with spaces or line breaks. Follow [YAML syntax](https://yaml-multiline.info/).

```yaml
hosts:
  list1: site1.com site2.com site3.net
  list2: |
    site1.com static.site1.cc
    site2.com cdnsite2.com
    site3.in site3.com site3.co.uk
```

## Module: DNS

Run DNS server and act as DNS proxy.

- Can protect from MITM DNS attack using [DNS over HTTPS](https://en.wikipedia.org/wiki/DNS_over_HTTPS) 
- Can work as AdBlock like [AdGuard](https://adguard.com/)

Enable server:

```yaml
dns:
  listen: ":53"
```

Rules action supports setting `static address` only:

- Useful for ad blocking.
- Useful for routing some sites traffic through pnproxy.

```yaml
dns:
  rules:
    - name: adblocklist
      action: static address 127.0.0.1
    - name: list1 list2 site4.com site5.net
      action: static address 192.168.1.123
```

Default action support some DoH providers:

- Without this configuration - DNS, HTTP, TLS modules may not work properly.

```yaml
dns:
  default:
    # provider - cloudflare, dnspod, google, quad9
    # cache - true (default false)
    action: doh provider cloudflare cache true
```

Total config:

```yaml
dns:
  listen: ":53"
  rules:
    - name: adblocklist
      action: static address 127.0.0.1
    - name: list1 list2 site4.com site5.net
      action: static address 192.168.1.123
  default:
    action: doh provider cloudflare cache true
```

## Module: HTTP

Run HTTP server and act as reverse proxy.

Enable server:

```yaml
http:
  listen: ":80"
```

Rules action supports setting `redirect scheme https` with optional code:

- Useful for redirect all sites traffic to TLS module.

```yaml
http:
  rules:
    - name: list1 list2 site4.com site5.net
      # code - any number (default - 307)
      action: redirect scheme https
```

Rules action supports setting `raw_pass`:

```yaml
http:
  rules:
    - name: list1 list2 site4.com site5.net
      action: raw_pass
```

Rules action supports setting `proxy_pass`:

- Useful for passing all sites traffic to additional local or remote proxy.

```yaml
http:
  rules:
    - name: list1 list2 site4.com site5.net
      # host and port - mandatory
      # username and password - optional
      # type - socks5 (default - http)
      action: proxy_pass host 123.123.123.123 port 3128 username user1 password pasw1
```

Default action support all rules actions:

```yaml
http:
  default:
    action: raw_pass  # default block
```

## Module: TLS

Run TCP server and act as Layer 4 reverse proxy.

Enable server:

```yaml
tls:
  listen: ":443"
```

Rules action supports setting `raw_pass`:

- Useful for forward HTTPS traffic to another reverse proxies with custom port.

```yaml
tls:
  rules:
    - name: list1 list2 site4.com site5.net
      # host - optional rewrite connection IP-address
      # port - optional rewrite connection port
      action: raw_pass host 123.123.123.123 port 10443
```

Rules action supports setting `proxy_pass`:

- Useful for passing all sites traffic to additional local or remote proxy.

```yaml
tls:
  rules:
    - name: list1 list2 site4.com site5.net
      # host and port - mandatory
      # username and password - optional
      # type - socks5 (default - http)
      action: proxy_pass host 123.123.123.123 port 3128 username user1 password pasw1
```

Rules action supports setting `split_pass`:

- Can try to protect from hardware MITM HTTPS attack.

```yaml
tls:
  rules:
    - name: list1 list2 site4.com site5.net
      # sleep - X/Y format: every X bytes sleep for Y (ms, us, ns)
      action: split_pass sleep 100/1ms
```

Default action support all rules actions:

```yaml
tls:
  default:
    action: raw_pass  # default block
```

## Module: Proxy

Run HTTP proxy server. This module does not have its own rules. It uses the HTTP and TLS module rules.
You can choose not to run DNS, HTTP, and TLS servers and use pnproxy only as HTTP proxy server.

Enable server:

```yaml
proxy:
  listen: ":8080"

dns:
   default: ...
http:
  rules: ...
  default: ...
tls:
  rules: ...
  default: ...
```
