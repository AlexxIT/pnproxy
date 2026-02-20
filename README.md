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

# Install

- Binary - [nightly.link](https://nightly.link/AlexxIT/pnproxy/workflows/build/master)
- Docker - [alexxit/pnproxy](https://hub.docker.com/r/alexxit/pnproxy)
- Home Assistant Add-on - [alexxit/hassio-addons](https://github.com/AlexxIT/hassio-addons)

# Setup

For example, you want to block ads and also forward all Twitter traffic through external proxy server.
And want it to work on all home devices without additional configuration on each device.

In the examples below, you installed pnproxy on your home server `192.168.1.2`. And you have an additional proxy server at `192.168.1.3:18080`.

1. Install pnproxy on any server in your home network.
   It is important that ports **53**, **80** and **443** be free on this server.
2. Create `pnproxy.yaml`
   ```yaml
   hosts:
     adblock: doubleclick.net googlesyndication.com
     tunnel: twitter.com twimg.com t.co x.com
   
   dns:
     listen: ":53"
     rules:
       - name: adblock                         # name from hosts block
         action: static address 127.0.0.1      # block this sites
       - name: tunnel                          # name from hosts block
         action: static address 192.168.1.2    # redirect this sites to pnproxy
     default:
       action: doh provider google             # resolve DNS for all other sites
   
   http:
     listen: ":80"
     rules:
       - name: tunnel                          # name from hosts block
         action: redirect scheme https         # redirect this sites from HTTP to TLS module
     default:
       action: raw_pass
   
   tls:
     listen: ":443"
     rules:
       - name: tunnel                          # name from hosts block
         action: proxy_pass host 192.168.1.3 port 18080  # forward this sites to external HTTP proxy
     default:
       action: raw_pass
   
   proxy:
     listen: ":3128"                           # optionally run local HTTP proxy
   
   log:
     level: trace                              # optionally increase log level (default - info)
   ```
3. Setup DNS server for your home router to `192.168.1.2`.

Optionally, instead of step 3, you can verify that everything works by configuring an HTTP proxy to `192.168.1.2:3128` on your PC or mobile device.

# Configuration

By default, the app looks for the `pnproxy.yaml` file in the current working directory.

```shell
pnproxy -config /config/pnproxy.yaml
```

By default, all modules disabled and don't listen any ports.

# Module: Hosts

Store lists of site domains for use in other modules.

- Name comparison includes all subdomains, you don't need to specify them separately!
- Names can be written with spaces or line breaks. Follow [YAML syntax](https://yaml-multiline.info/).

```yaml
hosts:
  list1: site1.com site2.com site3.net
  list2:
    site1.com static.site1.cc
    site2.com cdnsite2.com
    site3.in site3.com site3.co.uk
```

## hosts: http_get

This is a very powerful option that allows you to automatically determine the list of blocked websites.
For example, for the domain `google.com`, access to the page `https://google.com/` will be checked.

Check with timeout:

```yaml
hosts:
  http_error: http_get timeout 3
```

Sometimes it is important to read a few bytes (64000 in the example):

```yaml
hosts:
  http_error: http_get timeout 3 read_body 64000
```

Sometimes it is important to look at the HTTP response status (403 in the example):

```yaml
hosts:
  http_error: http_get timeout 3 read_body 64000 status 403
```

And the best thing is to check if this site works through a proxy:

```yaml
hosts:
  http_error: http_get timeout 3 read_body 64000 status 403 proxy http://192.168.1.3:18080
```

Finally, pnproxy will check whether the site is accessible, whether several bytes of the main page can be read, and what HTTP status the server returns.
If there are any problems, pnproxy will check whether the same site works through a proxy.
If a proxy fixes problems accessing a site, it will be marked with the `http_error` rule (you can change the name).

## hosts example

```yaml
hosts:
  adblock: doubleclick.net googlesyndication.com
  http_error: http_get timeout 3 read_body 64000 status 403 proxy http://192.168.1.3:18080  # change to proxy server

dns:
  listen: ":53"
  rules:
    - name: adblock
      action: static address 127.0.0.1
    - name: http_error
      action: static address 192.168.1.2  # change to pnproxy server
  default:
    action: doh provider google

http:
  listen: ":80"
  rules:
    - name: http_error
      action: redirect scheme https

tls:
  listen: ":443"
  rules:
    - name: http_error
      action: proxy_pass host 192.168.1.3 port 18080  # change to proxy server
```

# Module: DNS

Run DNS server and act as DNS proxy.

- Can protect from MITM DNS attack using [DNS over TLS](https://en.wikipedia.org/wiki/DNS_over_TLS) or [DNS over HTTPS](https://en.wikipedia.org/wiki/DNS_over_HTTPS) 
- Can work as AdBlock like [AdGuard](https://adguard.com/)

## dns listen

Enable server:

```yaml
dns:
  listen: ":53"
```

## dns action: static

Rules action supports setting `static address` only:

- Useful for ad blocking.
- Useful for routing some sites traffic through pnproxy.

```yaml
dns:
  rules:
    - name: adblocklist
      action: static address 127.0.0.1
    - name: list1 list2
      action: static address 192.168.1.2
```

## dns action: dns

Default action supports [DNS](https://en.wikipedia.org/wiki/Domain_Name_System), [DOT](https://en.wikipedia.org/wiki/DNS_over_TLS) and [DOH](https://en.wikipedia.org/wiki/DNS_over_HTTPS) upstream:

- Important to use server IP-address, instead of a domain name

```yaml
dns:
  default:
    action: dns server 8.8.8.8
```

Support build-in providers - `cloudflare`, `google`, `quad9`, `opendns`, `yandex`:

```yaml
dns:
  default:
    action: dns provider google
```

## dns action: dot

```yaml
dns:
  default:
    action: dot provider google
```

## dns action: doh

```yaml
dns:
  default:
    action: doh provider google
```

## dns example

Total config:

```yaml
dns:
  listen: ":53"
  rules:
    - name: adblocklist
      action: static address 127.0.0.1
    - name: list1 list2
      action: static address 192.168.1.2
  default:
    action: doh provider cloudflare
```

# Module: HTTP

Run HTTP server and act as reverse proxy.

Enable server:

```yaml
http:
  listen: ":80"
```

## http action: redirect

Rules action supports setting `redirect scheme https` with optional code:

- Useful for redirect all sites traffic to TLS module.

```yaml
http:
  rules:
    - name: list1 list2
      # code - any number (default - 307)
      action: redirect scheme https
```

## http action: raw_pass

Rules action supports setting `raw_pass`:

```yaml
http:
  rules:
    - name: list1 list2
      action: raw_pass
```

## http action: proxy_pass

Rules action supports setting `proxy_pass`:

- Useful for passing all sites traffic to additional local or remote proxy.

```yaml
http:
  rules:
    - name: list1 list2
      # host and port - mandatory
      # username and password - optional
      # type - socks5 (default - http)
      action: proxy_pass host 123.123.123.123 port 3128 username user1 password pasw1 type socks5
```

## http default action

Default action support all rules actions:

```yaml
http:
  default:
    action: raw_pass
```

# Module: TLS

Run TCP server and act as Layer 4 reverse proxy.

## tls listen

Enable server:

```yaml
tls:
  listen: ":443"
```

## tls action: raw_pass

Rules action supports setting `raw_pass`:

- Useful for forward HTTPS traffic to another reverse proxies with custom port.

```yaml
tls:
  rules:
    - name: list1 list2
      # host - optional rewrite connection IP-address
      # port - optional rewrite connection port
      action: raw_pass host 123.123.123.123 port 10443
```

## tls action: proxy_pass

Rules action supports setting `proxy_pass`:

- Useful for passing all sites traffic to additional local or remote proxy.

```yaml
tls:
  rules:
    - name: list1 list2
      # host and port - mandatory
      # username and password - optional
      # type - socks5 (default - http)
      action: proxy_pass host 123.123.123.123 port 3128 username user1 password pasw1
```

## tls default action

Default action support all rules actions:

```yaml
tls:
  default:
    action: raw_pass
```

# Module: Proxy

Run HTTP proxy server. This module does not have its own rules. It uses the HTTP and TLS module rules.
You can choose not to run DNS, HTTP, and TLS servers and use pnproxy only as HTTP proxy server.

## proxy listen

Enable server:

```yaml
proxy:
  listen: ":3128"
```

# Example

```yaml
log:
  level: info

hosts:
  # list of sited to block
  adblock: doubleclick.net googlesyndication.com
  # forward some sited to proxy
  proxy: twitter.com twimg.com t.co x.com
  # auto-detect blocked sited via proxy server
  error: http_get timeout 3 read_body 64000 status 403 proxy http://192.168.1.3:18080

dns:
  listen: ":53"
  rules:
    # block this sited
    - name: adblock
      action: static address 127.0.0.1
    # forward this sited to pnproxy server IP
    - name: proxy error
      action: static address 192.168.1.2
  default:
    action: doh provider google

http:
  listen: ":80"
  rules:
    # redirect HTTP requests to HTTPS
    - name: proxy error
      action: redirect scheme https

tls:
  listen: ":443"
  rules:
    # forward this sites to proxy server
    - name: proxy error
      action: proxy_pass host 192.168.1.3 port 18080

proxy:
  # use pnproxy as proxy server for testing purposes
  listen: ":3128"
```

# Tips and Tricks

**Mikrotik DNS fail over script**

- Add as System > Scheduler > Interval `00:01:00`

```
:global server "192.168.1.2"

:do {
  :resolve google.com server $server
} on-error={
  :global server "8.8.8.8"
}

:if ([/ip dns get servers] != $server) do={
  /ip dns set servers=$server
}
```

# Known bugs

In rare cases, due to [HTTP/2 connection coalescing](https://blog.cloudflare.com/connection-coalescing-experiments) technology, some site may not work properly when using a TCP/TLS Layer 4 proxy. In HTTP proxy mode everything works fine. Everything works fine in Safari browser (it doesn't support this technology). In Firefox, this feature can be disabled - `network.http.http2.coalesce-hostnames`.
