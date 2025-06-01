![Build Status](https://github.com/ricardobranco777/unpage/actions/workflows/ci.yml/badge.svg)

# go-unpage
Get all pages from a paginated JSON API URL

Python version: https://github.com/ricardobranco777/py-unpage

## Usage

```
Usage: ./unpage [OPTIONS] URL
  -D, --data-key string     key to access the data in the JSON response
  -H, --header strings      HTTP header (may be specified multiple times
  -L, --last-key string     key to access the last page link in the JSON response
  -N, --next-key string     key to access the next page link in the JSON response
  -P, --param-page string   parameter that represents the page number
      --version             print version and exit
```

## Examples

```
unpage --data-key repositories https://registry.opensuse.org/v2/_catalog?n=50

unpage --param-page page https://src.opensuse.org/api/v1/repos/issues/search?limit=1

unpage --param-page page --data-key issues_created --next-key pagination_issues_created.next --last-key pagination_issues_created.last 'https://code.opensuse.org/api/0/user/rbranco/issues?assignee=1&per_page=1'
```
