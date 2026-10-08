# Security policy

## Supported versions

Security fixes are made on the latest minor release. Before `v1.0.0`, that is the most recent `v0.x` release.

## Reporting a vulnerability

Please do not open a public issue.

Report privately through GitHub: on this repository, open the **Security** tab and choose **Report a vulnerability**. If you can't use GitHub, email **hello@onaro.io** with "Security" in the subject.

Include the affected version, a description of the issue, and steps to reproduce if you have them. We aim to acknowledge reports within three business days and to agree a disclosure date with you once a fix is ready.

## Scope notes

- `record_id` values are derived deterministically from trace and span IDs. They are identifiers, not secrets, and are predictable by design.
- The http sink sends its bearer token on every request. Use an `https://` endpoint and keep the token in an environment variable or secret store rather than in the config file.
