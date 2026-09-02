# syntax=docker/dockerfile:1.7@sha256:a57df69d0ea827fb7266491f2813635de6f17269be881f696fbfdf2d83dda33e
FROM scratch
COPY --chmod=0555 push-service /push-service
USER 65532:65532
ENTRYPOINT ["/push-service"]
