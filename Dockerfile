FROM gcr.io/distroless/static-debian12:nonroot

USER 20000:20000
COPY --chmod=555 external-dns-efficientip-webhook /opt/external-dns-efficientip-webhook/app

ENTRYPOINT ["/opt/external-dns-efficientip-webhook/app"]
