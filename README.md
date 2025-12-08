# Go Playground

# General Developer Notes

## How to grep filenames with extensions

```bash
grep -R -l \
  --include='*.yml' --include='*.yaml' \
  --exclude-dir='testdata' \
  -e . .
```


