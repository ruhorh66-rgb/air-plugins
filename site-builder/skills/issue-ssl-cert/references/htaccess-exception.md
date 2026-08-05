# Redirect-only domain: exempt the ACME challenge path

If a domain's whole job is a 301 redirect (e.g. an old domain now pointed at
a new canonical one via `.htaccess`), Let's Encrypt's HTTP-01 validator will
follow the redirect instead of finding the challenge token, and validation
fails. Add an exception **above** the redirect rule so the challenge path is
served directly:

```apache
RewriteEngine On
RewriteCond %{REQUEST_URI} ^/\.well-known/acme-challenge/ [NC]
RewriteRule ^ - [L]
RewriteCond %{HTTP_HOST} ^(www\.)?example\.com$ [NC]
RewriteRule ^(.*)$ https://canonical-domain/$1 [R=301,L]
```

The exception is harmless to leave in place afterward — it only ever matches
requests under `/.well-known/acme-challenge/`, which don't otherwise occur in
normal traffic.
