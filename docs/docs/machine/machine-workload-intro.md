# Machine Workloads

A Machine Workload is a set of shell scripts that run on a Machine Runtime
Instance over the existing SSH connection.  This is the workload model for
hosts that live outside Kubernetes, such as virtual machines, bare-metal
servers, and edge devices.

## Machine Workload Definition

The definition holds the scripts and execution settings for a machine
workload.  Three scripts drive the lifecycle:

* `CreateScript` (required) runs when an instance is created
* `UpdateScript` (optional) runs when an instance is updated
* `DeleteScript` (required) runs when an instance is deleted

Each script runs through `<shell> -s` (default `/bin/bash`), optionally
preceded by a `cd <WorkingDir>`.  `Timeout` sets a per-script deadline in
seconds.

Scripts can be retried by the reconciler on transient failure (e.g. the
host briefly unreachable), so author them to be idempotent, safe to rerun
without harm.

The `Env` field is a list of `KEY=VALUE` entries.  Values are encrypted at
rest.

## Machine Workload Instance

A machine workload instance binds a definition to a machine runtime
instance.  The instance may override the definition's `Env` by setting its
own.  The instance also records the latest reconciler-observed status from
the most recent script run.

