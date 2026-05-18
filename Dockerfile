FROM gcr.io/distroless/static-debian12:nonroot
COPY burrowd /usr/local/bin/burrowd
COPY burrow        /usr/local/bin/burrow
EXPOSE 7000 8080 9000-9100
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/burrowd"]
CMD ["serve"]
