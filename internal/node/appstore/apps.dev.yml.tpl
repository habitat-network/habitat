- app_installation:
    name: pouch_frontend
    version: 4
    driver: web

    driver_config:
      download_url: https://github.com/eagraf/pouch/releases/download/release-3/dist.tar.gz
      bundle_directory_name: pouch

    registry_url_base: a
    registry_app_id: b
    registry_tag: c

  reverse_proxy_rules:
    - type: file
      matcher: /pouch
      target: pouch/4/dist
    - type: redirect
      matcher: /pouch_api
      target: http://host.docker.internal:6000
# TODO: uncomment once fishtail ingestion is merged
#    - type: fishtail_ingest
#      matcher: /pouch_api/ingest
#      target: http://host.docker.internal:6000/api/v1/ingest
#      fishtail_ingest_config:
#        subscribed_collections:
#          - lexicon: app.bsky.feed.like
#          - lexicon: com.habitat.pouch.link
- app_installation:
    name: pouch_backend
    version: 1
    driver: docker

    driver_config:
      env:
        - PORT=6000
      mounts:
        - type: bind
          source: {{.HabitatPath}}/apps/pouch/database.sqlite
          target: /app/database.sqlite
      exposed_ports:
        - "6000"
      port_bindings:
        "6000/tcp":
          - HostIp: "0.0.0.0"
            HostPort: "6000"

    registry_url_base: registry.hub.docker.com
    registry_app_id: ethangraf/pouch-backend
    registry_tag: release-3