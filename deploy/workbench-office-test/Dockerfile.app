# Test-only runtime; uses the same embedded server as the normal release.
ARG RUNTIME_IMAGE
FROM ${RUNTIME_IMAGE}
RUN apk add --no-cache ca-certificates tzdata libwebp-tools \
 && addgroup -g 11001 office && adduser -D -u 11001 -G office office
WORKDIR /app
COPY sub2api /app/sub2api
COPY resources /app/resources
RUN mkdir -p /app/data && chown office:office /app/data
USER 11001:11001
EXPOSE 8080
CMD ["/app/sub2api"]
