# The sandbox smoke bundle: the compiled wave 1 acceptance tests, the sweep, Terraform and
# the entrypoint. The build context is the directory scripts/smoke-bundle/build.sh stages.
# Alpine is the smallest maintained base that has /bin/sh, a CA bundle and a world-writable
# /tmp. 3.24 is the newest release branch; bump the digest with it.
FROM alpine:3.24@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6

RUN adduser -D -u 10001 smoke

COPY --chmod=0755 bin/terraform bin/test2json /usr/local/bin/
COPY --chmod=0755 bin/sweep.test /smoke/sweep.test
# plugin-testing links the test binary's directory into each test's working directory and
# sets its mode, which needs the directory to belong to the user the tests run as.
COPY --chown=smoke:smoke --chmod=0755 bin/acc/ /smoke/acc/
COPY --chmod=0755 entrypoint.sh /smoke/entrypoint.sh

USER smoke
WORKDIR /smoke
ENTRYPOINT ["/smoke/entrypoint.sh"]
