# Domains, DNS und Netzwerk

Diese Anleitung beschreibt, wie du TEX Share mit praktisch jeder DNS- und
Netzwerkumgebung betreibst. **Cloudflare ist eine bequeme Option, aber niemals
Voraussetzung.** Ebenso wenig wird ein bestimmter Reverse Proxy erzwungen.

Alle Einstellungen findest du im Admin-Panel unter
**Einstellungen → Domains & Netzwerk** (Assistent) sowie pro Domain unter
**Verwaltung → Domains** (Schaltfläche *DNS & Netzwerk*).

---

## 1. Grundkonzept

TEX Share trennt drei voneinander unabhängige Bereiche:

| Bereich | Frage | Optionen |
| --- | --- | --- |
| **DNS-Verwaltung** | Wo und wie werden DNS-Records verwaltet? | Cloudflare API · Manuell · Keine |
| **Netzwerkverbindung** | Wie erreicht eine Anfrage den Server? | Direkt · Externer Proxy · Tunnel · Statische IP · DynDNS |
| **Reverse Proxy** | Terminiert ein externer Proxy TLS? | Keiner · Nginx · Caddy · Traefik · NPM · HAProxy · Anderer |

Diese Bereiche sind entkoppelt. Beliebige Kombinationen sind möglich, zum
Beispiel:

- Cloudflare DNS + Cloudflare Tunnel
- Cloudflare DNS + eigener Reverse Proxy
- Manuelle DNS-Konfiguration + Nginx / Caddy / Traefik / NPM / HAProxy
- Beliebiger DNS-Anbieter + externer Proxy
- Heimserver mit dynamischer IP + DynDNS
- Server mit statischer IPv4/IPv6
- Heimserver hinter CGNAT/DS-Lite + Cloudflare Tunnel

## 2. Ports und Routing

TEX Share läuft immer als **ein Container** und hört auf zwei getrennten Ports:

| Oberfläche | Port | Zweck |
| --- | --- | --- |
| **Public UI** | `3535` | Freigabelinks, Download, öffentliche API |
| **Admin UI** | `3636` | Verwaltung, Einrichtung — **privat halten** |

Beide Ports sind standardmäßig auf `0.0.0.0` gebunden. Ohne Cloudflare, Tunnel
oder Reverse Proxy funktioniert TEX Share direkt über `3535` und `3636`.

Der Reverse Proxy (oder Tunnel) leitet ausschließlich auf **Port 3535** weiter.
Die Admin-Oberfläche wird **nicht** automatisch über einen Proxy freigegeben.

```
Internet ──▶ Reverse Proxy ──▶ TEX Share 127.0.0.1:3535
                                  (Public UI)
Admin-Zugriff (LAN/VPN/explizit) ──▶ TEX Share :3636
```

## 3. DNS-Anbindung

### 3.1 Cloudflare API (optional)

Damit TEX Share DNS-Records automatisch anlegen und aktualisieren kann,
hinterlegst du einen **API-Token**. Erstelle ihn im Cloudflare-Dashboard unter
*My Profile → API Tokens → Create Token* mit **minimalen Rechten**:

| Scope | Berechtigung |
| --- | --- |
| Zone → DNS | Edit |
| Zone → Zone | Read |

Der Token wird mit dem Master-Key verschlüsselt in der Datenbank gespeichert
und **niemals geloggt**. Im Assistenten kannst du die Verbindung testen und die
verfügbaren Zonen auswählen. Unterstützt werden **A-, AAAA- und CNAME-Records**.

TEX Share ändert ausschließlich Records, die es selbst verwaltet. Fremde Records
werden nicht angetastet.

### 3.2 Manuelle DNS-Konfiguration

Für jeden DNS-Anbieter ohne API (oder wenn du die Kontrolle behalten willst):
Wähle *Manuelle DNS-Konfiguration*. TEX Share zeigt dann die exakt benötigten
Einträge als Tabelle an — es wird **nichts** automatisch erstellt.

Beispiel für `share.maxi.xyz` bei direkter Verbindung:

| Typ | Name | Ziel |
| --- | --- | --- |
| A | `share` | Öffentliche IPv4 |
| AAAA | `share` | Öffentliche IPv6 (optional) |

Bei einem externen Reverse Proxy oder Tunnel zeigt der Assistent stattdessen
passende **CNAME**- bzw. Proxy-Einträge an.

Nach der manuellen Einrichtung startest du unter *Domains* die Prüfung
(*DNS prüfen*). TEX Share behauptet nie, einen Eintrag angelegt zu haben, wenn
der Anbieter keine API besitzt.

### 3.3 Keine automatische DNS-Verwaltung

Wenn DNS vollständig extern verwaltet wird, wähle *Keine automatische
DNS-Verwaltung*. TEX Share berührt DNS dann überhaupt nicht; Domains dienen nur
der Freigabe-Zuordnung.

## 4. Netzwerkverbindung

### 4.1 Direkte Portweiterleitung

Der Server ist über eine öffentliche IPv4/IPv6 direkt erreichbar. Leite im
Router **TCP 3535** auf den Server weiter (und `3636` **nicht** ins Internet).
Der DNS-Eintrag zeigt auf die öffentliche Adresse.

> Router- oder Firewall-Einstellungen ändert TEX Share **nie** automatisch.
> Portfreigaben richtest du selbst ein.

### 4.2 Externer Reverse Proxy

Ein Proxy (Nginx, Caddy, Traefik, NPM, HAProxy) terminiert TLS und leitet auf
`127.0.0.1:3535` (bzw. den TEX-Share-Host) weiter. Details in Abschnitt 5.

### 4.3 Cloudflare Tunnel

Der Tunnel baut eine **ausgehende** Verbindung auf und funktioniert daher auch
hinter CGNAT oder DS-Lite, ohne offene Ports. Einrichtung unter
**Einstellungen → Cloudflare** (Browser-Login, kein API-Token für den Tunnel
nötig). TEX Share verwaltet Tunnel, DNS-Eintrag und Ingress-Regeln und
überwacht den `cloudflared`-Prozess. Optional kombinierbar mit Cloudflare DNS.

### 4.4 Statische öffentliche IP

Der Server hat eine feste Adresse. Der DNS-Eintrag zeigt direkt darauf; keine
automatische Aktualisierung nötig.

### 4.5 Dynamische IP / DynDNS

Der integrierte DynDNS-Dienst:

- erkennt öffentliche IPv4 und IPv6,
- stellt Änderungen fest,
- aktualisiert A-/AAAA-Records **nur bei tatsächlicher Abweichung**,
- prüft in einem konfigurierbaren Intervall (Minuten),
- versucht es bei Fehlern erneut,
- zeigt letzte erfolgreiche Aktualisierung, aktuelle IP und Fehler an,
- protokolliert alle Vorgänge (ohne Zugangsdaten).

Die Architektur ist modular — Cloudflare ist der erste unterstützte Anbieter,
weitere folgen. Bei DNS-Anbietern **ohne** API bleibt DynDNS manuell (z. B. über
den Router oder einen externen DynDNS-Dienst).

> **Wichtig:** DynDNS ersetzt keine öffentliche Erreichbarkeit. Hinter **CGNAT**
> oder **DS-Lite** ist der Anschluss von außen nicht erreichbar — nutze dort
> einen Tunnel, eine geeignete IPv6-Verbindung oder einen externen Server.

## 5. Reverse-Proxy-Konfiguration

Alle Beispiele sind auf das echte Portkonzept abgestimmt: Upstream ist
**`http://127.0.0.1:3535`**, TLS wird am Proxy terminiert.

Gemeinsame Pflichtpunkte für **jeden** Proxy:

- **Host-Header** unverändert weitergeben.
- **HTTPS-Info** setzen (`X-Forwarded-Proto: https`).
- **Forwarded-Header** korrekt setzen (`X-Forwarded-For`, `X-Forwarded-Host`).
- **Kein Request-Size-Limit** (`client_max_body_size 0` bzw. äquivalent).
- **Kein Puffern** von Request-/Response-Body (Streaming großer Dateien).
- **Lange Timeouts** (≥ 1 h) für große Uploads/Downloads.
- **Range-/If-Range-Header** durchreichen (Resume, Video-Seeking).

> Resumable Uploads und große Dateiübertragungen dürfen nicht durch
> Proxy-Timeouts oder Request-Size-Limits verhindert werden.

### 5.1 Nginx

```nginx
# /etc/nginx/conf.d/texshare.conf
server {
    listen 443 ssl http2;
    server_name share.example.com;

    ssl_certificate     /etc/letsencrypt/live/share.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/share.example.com/privkey.pem;

    # Große und resumable Uploads zulassen.
    client_max_body_size 0;
    proxy_request_buffering off;
    proxy_buffering off;

    location / {
        proxy_pass http://127.0.0.1:3535;
        proxy_http_version 1.1;

        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Forwarded-Host  $host;

        proxy_set_header Range    $http_range;
        proxy_set_header If-Range $http_if_range;

        proxy_connect_timeout 60s;
        proxy_send_timeout    3600s;
        proxy_read_timeout    3600s;
    }
}
```

Trage die Proxy-Adresse (z. B. `127.0.0.1/32`) unter **Vertrauenswürdige
Proxies** ein.

### 5.2 Caddy

```caddy
# Caddyfile
share.example.com {
    encode zstd gzip

    reverse_proxy 127.0.0.1:3535 {
        header_up Host              {host}
        header_up X-Forwarded-For   {remote_host}
        header_up X-Forwarded-Proto {scheme}
        header_up X-Forwarded-Host  {host}

        flush_interval -1

        transport http {
            dial_timeout  10s
            read_timeout  1h
            write_timeout 1h
        }
    }
}
```

Caddy besorgt TLS-Zertifikate automatisch und puffert Request-Bodies
standardmäßig nicht.

### 5.3 Traefik

```yaml
# Dynamische Konfiguration (file provider)
http:
  routers:
    texshare:
      rule: "Host(`share.example.com`)"
      entryPoints: [websecure]
      service: texshare
      tls:
        certResolver: letsencrypt

  services:
    texshare:
      loadBalancer:
        passHostHeader: true
        servers:
          - url: "http://127.0.0.1:3535"
        responseForwarding:
          flushInterval: 100ms
```

`passHostHeader: true` erhält den Host-Header; Traefik setzt `X-Forwarded-*`
selbst. Bei Docker-Betrieb die Proxy-Netz-CIDR eintragen.

### 5.4 Nginx Proxy Manager (NPM)

*Add Proxy Host*:

| Feld | Wert |
| --- | --- |
| Domain Names | `share.example.com` |
| Scheme | `http` |
| Forward Hostname / IP | `127.0.0.1` (oder TEX-Share-Container) |
| Forward Port | `3535` |
| Block Common Exploits | an |
| Websockets Support | an |
| SSL | Let's Encrypt, Force SSL + HTTP/2 |

Im Tab *Advanced* die Custom-Konfiguration ergänzen:

```nginx
client_max_body_size 0;
proxy_request_buffering off;
proxy_buffering off;
proxy_set_header X-Forwarded-Proto $scheme;
proxy_set_header X-Forwarded-Host  $host;
proxy_read_timeout 3600s;
proxy_send_timeout 3600s;
```

NPM läuft selbst als Container — die NPM-Container-IP bzw. das Docker-Netz
unter **Vertrauenswürdige Proxies** eintragen. Admin-UI (`3636`) **nicht** als
Proxy Host anlegen.

### 5.5 HAProxy

```haproxy
# /etc/haproxy/haproxy.cfg (Auszug)
frontend https_in
    bind *:443 ssl crt /etc/haproxy/certs/share.example.com.pem
    http-request set-header X-Forwarded-Proto https
    http-request set-header X-Forwarded-Host  %[req.hdr(host)]
    default_backend texshare

backend texshare
    balance roundrobin
    option forwardfor
    option http-server-close
    http-reuse safe
    timeout connect 10s
    timeout server  1h
    timeout client  1h
    server texshare1 127.0.0.1:3535 check
```

### 5.6 Anderer Proxy

Jeder standardkonforme HTTP(S)-Proxy funktioniert, solange Host- und
Forwarded-Header gesetzt werden, nicht gepuffert wird, kein Request-Size-Limit
gilt und Range-Header durchgereicht werden. Upstream: `http://127.0.0.1:3535`.

## 6. Header-Sicherheit und vertrauenswürdige Proxies

TEX Share übernimmt Client-Header **niemals blind**. Konfiguriere unter
**Vertrauenswürdige Proxies** die IPs/CIDRs deines Proxys (kommagetrennt), z. B.
`127.0.0.1/32, 172.16.0.0/12`.

Regeln:

- `X-Forwarded-For`, `X-Forwarded-Proto`, `X-Forwarded-Host` und `Forwarded`
  werden **nur** ausgewertet, wenn der unmittelbare Absender ein
  vertrauenswürdiger Proxy ist.
- `CF-Connecting-IP` wird nur von einem vertrauenswürdigen Peer akzeptiert.
- Von allen anderen Absendern werden diese Header vollständig ignoriert.
- **Host-Validierung und Domain-Allowlisting** bleiben aktiv.
- Bei leerer Liste werden Forwarded-Header gar nicht ausgewertet.

Die Umgebungsvariable `TEXSHARE_TRUSTED_PROXIES` setzt die Liste zusätzlich.
Die veraltete Variable `TEXSHARE_TRUST_PROXY=true` vertraut — falls keine Liste
gesetzt ist — nur Loopback (`127.0.0.1/32`, `::1/128`).

## 7. Getrennte öffentliche und administrative Oberfläche

Die Porttrennung bleibt zwingend:

- **Public UI (3535)** kann über beliebige Domains veröffentlicht werden.
- **Admin UI (3636)** wird **nicht** automatisch veröffentlicht.
- Admin-Erreichbarkeit ist separat möglich (LAN, VPN oder ein ausdrücklich
  eingerichteter Proxy). Die Option *Admin-UI über Proxy erreichbar* im
  Assistenten dokumentiert diese bewusste Freigabe.
- Beide Anwendungen behalten ihre **getrennte Authentifizierung** (eigene
  Cookies `texshare_pub` / `texshare_adm`).

## 8. Domainüberprüfung und Status

Pro Domain zeigt TEX Share einen Status:

| Prüfung | Bedeutung |
| --- | --- |
| DNS erkannt | Der Hostname löst auf. |
| Ziel korrekt | Der Record zeigt auf das erwartete Ziel. |
| Domain erreichbar | `/healthz` antwortet. |
| HTTPS aktiv | Antwort über TLS. |
| Zertifikat gültig | Zertifikat vorhanden, gültig, nicht abgelaufen. |
| Reverse Proxy erkannt | Server-/Via-Header deuten auf einen Proxy. |
| Public UI erreichbar | Antwort stammt von TEX Share. |
| Admin UI erreichbar | Nur falls konfiguriert. |

Fehler werden im Klartext mit konkreten Lösungsvorschlägen angezeigt. Die
Prüfung nutzt einen **SSRF-sicheren** Dialer, der private und Loopback-Ziele
blockiert (außer bei ausdrücklich lokalen Modi).

## 9. Heimserver, CGNAT und DS-Lite

- **Dynamische öffentliche IP:** Netzwerkverbindung *Dynamische IP / DynDNS* +
  DynDNS aktivieren. DNS-Provider mit API (z. B. Cloudflare) aktualisiert
  A/AAAA automatisch.
- **CGNAT / DS-Lite:** Kein eingehender Port möglich. Nutze den **Cloudflare
  Tunnel** (ausgehend) oder eine **IPv6**-Verbindung, bei der eingehende
  Verbindungen erlaubt sind. DynDNS allein genügt hier nicht.
- **NAS / Heimserver:** Externer Reverse Proxy auf dem NAS oder ein Tunnel ist
  meist der einfachste Weg.

## 10. IPv4 und IPv6

- A-Records zeigen auf IPv4, AAAA-Records auf IPv6.
- DynDNS kann beide Familien unabhängig aktualisieren (IPv4/IPv6-Schalter).
- Achte darauf, dass der Client tatsächlich über die Familie verbindet, die du
  veröffentlichst (Firewall, Router).
- IPv6 allein ist eine gute Lösung hinter CGNAT, sofern dein Anschluss ein
  **globales, erreichbares Präfix** hat.

## 11. TLS und HTTPS

- TLS wird am Reverse Proxy oder von Cloudflare terminiert; TEX Share spricht
  intern HTTP.
- Setze `TEXSHARE_PUBLIC_URL` / `TEXSHARE_ADMIN_URL` auf die echten URLs und
  `TEXSHARE_FORCE_SECURE_COOKIES=true`.
- Trage die Proxy-CIDR als vertrauenswürdig ein, damit `X-Forwarded-Proto`
  ausgewertet wird.
- Let's Encrypt (Nginx/Caddy/NPM/Traefik) oder Cloudflare-Edge-Zertifikate sind
  gängige Wege.

## 12. Sichere Admin-Erreichbarkeit

- Admin-Port **nicht** ins Internet freigeben.
- Zugriff über **LAN** oder **VPN**.
- Falls doch über einen Proxy: nur mit starker Authentifizierung (mTLS, IP-
  Allowlist, vorgeschaltete Authentifizierung) und nur, wenn du im Assistenten
  *Admin-UI über Proxy erreichbar* bewusst aktivierst.
- `TEXSHARE_ADMIN_ALLOWED_HOSTS` einschränken und 2FA für Admins aktivieren.

## 13. Fehlerbehebung

| Symptom | Ursache / Lösung |
| --- | --- |
| „invalid_host“ | Hostname nicht in Domains registriert oder nicht in `TEXSHARE_ALLOWED_HOSTS`. |
| Client-IP ist die Proxy-IP | Proxy-CIDR fehlt unter *Vertrauenswürdige Proxies*. |
| Cookies werden nicht gesetzt | `X-Forwarded-Proto` fehlt / Proxy nicht vertrauenswürdig; `TEXSHARE_FORCE_SECURE_COOKIES` prüfen. |
| Upload bricht ab | Proxy-Timeout zu kurz, Pufferung aktiv oder Size-Limit gesetzt. |
| „DNS-Ziel falsch“ | Record zeigt nicht auf die öffentliche Adresse bzw. den Proxy-/Tunnel-Hostnamen. |
| DNS-Record wird nicht angelegt | Manueller Anbieter (keine API) — Einträge selbst erstellen. |
| Domain hinter CGNAT nicht erreichbar | Tunnel oder IPv6 nutzen. |

---

Weitere Dokumente: [`INSTALL.md`](INSTALL.md), [`BACKUP.md`](BACKUP.md),
[`UPDATE.md`](UPDATE.md), [`SECURITY.md`](SECURITY.md).
