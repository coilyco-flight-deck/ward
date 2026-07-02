# container lifecycle logs

Ward now emits stable stderr markers during container dispatch, bootstrap, and
reap. Use them when a headless run needs a later reconstruction from `console.log`.

Greps to start with:

- `ward agent:` - dispatch, pre-flight, launch handoff.
- `ward container:` - bootstrap and reap decisions.
- `ward container reap:` - residual status, push, salvage, and extra-repo checks.
