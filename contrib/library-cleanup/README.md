# Library cleanup

Tools for cleaning up a music share before the app scans it. The app itself has no dedupe code ([ADR-0002](../../docs/adr/0002-quarantine-duplicates-instead-of-in-app-dedupe.md)); these run by hand, through a temporary writable mount, and the app keeps its read-only mount throughout.

| Tool | What it does |
|---|---|
| `scan.py` | Lists every folder and File on the share, serially, as TSV |
| `casemerge.py` | Plans merges of sibling names that differ only by letter case (pure, unit-tested) |
| `apply.py` | Carries out a plan, checking each step by re-listing both folders |

Python 3.10+ only, no dependencies. Run the tests with `python3 -m unittest` in this folder.

## Why names that differ only by case need merging

Some NAS devices (old Buffalo LinkStations over SMB1, for example) look names up case-insensitively and cache the lookups. For two siblings such as `ATB` and `Atb`, both paths can lead into the *same* real folder, so a listing of either side shows the same Files and scans silently miss the other folder's contents. Mounting with SMB1 Unix extensions would make names case-sensitive, but these devices don't support them.

You can't merge such a pair by moving Files from one side to the other, because you can't tell which folder a path really reaches. So the merge has two phases:

- **Phase A: rename.** All but one member of each clash gets a unique temporary name (`Atb [case-merge]`). A rename can't lose data, whichever real folder the server applies it to. Clashes nested inside clashing folders only become visible once their parents have unique names, so Phase A repeats until a scan finds none.
- **Phase B: merge.** With every name unique, listings are trustworthy. Each `[case-merge]` sibling is merged into its partner. The side with more Files wins, so fewer Files move. A same-name File of the same size is planned for Quarantine, and `apply.py` moves it only after confirming it is byte-identical to the File it duplicates. Anything else is left for the owner to decide.

## Recipe

`$PLANS` is any folder outside the share for scans and plans. Never edit a plan after `apply.py` has started on it, because its progress log records step numbers.

**1. Mount the share writable alongside the read-only mount.** Use the same credentials file and SMB version as the normal mount (`NAS` and the credentials path are placeholders):

```bash
sudo mkdir -p /mnt/music-rw
sudo mount -t cifs //NAS/music /mnt/music-rw \
  -o rw,vers=1.0,credentials=/path/to/smb-credentials,uid=$(id -u),gid=$(id -g),file_mode=0644,dir_mode=0755,iocharset=utf8,noserverino
```

**2. Phase A, repeated until it plans nothing:**

```bash
python3 scan.py /mnt/music-rw > $PLANS/scan.tsv
python3 casemerge.py rename $PLANS/scan.tsv > $PLANS/rename-1.tsv   # review it
python3 apply.py /mnt/music-rw $PLANS/rename-1.tsv --dry-run
python3 apply.py /mnt/music-rw $PLANS/rename-1.tsv
```

Then rescan and plan again (`rename-2.tsv`, …) until `casemerge.py` reports `0 clashing groups`.

**3. Phase B:**

```bash
python3 scan.py /mnt/music-rw > $PLANS/scan.tsv
python3 casemerge.py merge $PLANS/scan.tsv > $PLANS/merge.tsv
```

`casemerge.py merge` refuses to plan while any names still clash. Review `merge.tsv` before applying it, and decide each `decide` line by hand. To keep one copy and drop the other, change the line to `set-aside<TAB>SRC<TAB>_Quarantine/KEEP<TAB>KEEP`: `apply.py` then moves SRC into Quarantine without the byte check, provided KEEP exists. Use the same change when `apply.py` stops because a planned quarantine isn't byte-identical (e.g. `.DS_Store` files, or copies whose tags differ). Changing a line that hasn't run yet keeps the step numbers, so the run can resume. Then run `apply.py` on it as in step 2. If `apply.py` stops on a step that won't verify, or a pair can't be addressed reliably over SMB at all, merge that pair on the NAS directly instead (e.g. through its web file manager), rescan, and continue.

**4. Check.** Two consecutive scans must finish without errors and return identical lists, with no clashes and no `[case-merge]` names left:

```bash
python3 scan.py /mnt/music-rw > $PLANS/check-1.tsv && \
python3 scan.py /mnt/music-rw > $PLANS/check-2.tsv && \
cmp $PLANS/check-1.tsv $PLANS/check-2.tsv && echo "scans identical"
python3 casemerge.py rename $PLANS/check-1.tsv > /dev/null   # must report "0 clashing groups"
grep -c 'case-merge' $PLANS/check-1.tsv                       # must print 0
```

Some NAS devices keep a recycle bin on the share (`trashbox/` on Buffalo LinkStations), and removing a folder over SMB moves it there instead of deleting it. The emptied `[case-merge]` folders end up there, so ignore that folder in step 4 and empty it from the NAS's own admin page.

**5. Unmount the writable mount:** `sudo umount /mnt/music-rw`. The app's read-only mount was never changed.
