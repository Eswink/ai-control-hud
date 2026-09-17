# Main integration gate

This file records the final Central Hub V2 integration gate after PR #91 was retargeted from the H29 stacked branch to `main`.

The retarget itself did not change the head commit, so this small documentation commit intentionally forces GitHub to rebuild the PR merge ref and re-run all required pull-request workflows against the actual `main` base.

Merge policy for this checkpoint:

- require Go Agent CI, Go Hub CI, Android CI, Go Agent Release, and Python CI to pass on the resulting PR head;
- merge only after those checks are green;
- keep previously excluded outage/reboot/DHCP/backup/endurance drills recorded as `NOT RUN` rather than silently treating CI as field evidence;
- do not retrieve or infer CommandCode credentials;
- production Android signing remains a separate release concern.
