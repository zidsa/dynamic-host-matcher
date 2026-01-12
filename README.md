# Caddy Dynamic Host Matcher

A Caddy HTTP matcher that dynamically loads host lists from HTTP endpoints. Perfect for SaaS applications where the list of valid hosts changes frequently.

## Features

- Dynamically fetches host lists from HTTP/HTTPS endpoints
- Configurable refresh intervals
- Thread-safe operation
- Graceful error handling with fallback to last known good state
- Support for wildcard patterns

## Installation

```bash
xcaddy build --with github.com/zidsa/caddy-dynamic-host-matcher
```

## Usage

### Caddyfile

```caddyfile
@dynamic_hosts {
    dynamic_host {
        source https://api.example.com/hosts
        interval 30s
    }
}

handle @dynamic_hosts {
    reverse_proxy backend:8080
}
```

### JSON Config

```json
{
    "match": [
        {
            "dynamic_host": {
                "source": "https://api.example.com/hosts",
                "interval": "30s"
            }
        }
    ]
}
```

## API Response Format

Your endpoint should return JSON in this format:

```json
{
    "hosts": [
        "example.com",
        "app1.example.com", 
        "*.wildcard.com"
    ]
}
```

## Configuration

- `source` (required): HTTP/HTTPS URL to fetch host lists from
- `interval` (optional): How often to refresh the host list (default: 30s, min: 1s, max: 24h)

## License

MIT License - see [LICENSE](LICENSE) file for details.