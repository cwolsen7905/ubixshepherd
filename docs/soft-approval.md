# Soft approval

Soft approval (an agent review that lets a merge keep moving) is not built.
There is no `autonomy.approve` setting, no `shepherd review` command, no review
job, and no audit log. Merging follows the repo's `autonomy.merge` rule as
[merging.md](merging.md) describes: only the person merges, or an agent arms
merge-when-pipeline-succeeds where the profile allows it. Agents never approve
their own work, and a soft approval by an agent never counts as the person's.
