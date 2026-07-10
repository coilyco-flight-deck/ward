# broker

The broker is the root credential relay for the director surface.

- it keeps the read-only director lane from needing the raw host token path.
- it carries the write credential only where the code says it should.
- it is part of the agent surface contract, not a generic network helper.

## See also

- [agent-director.md](agent-director.md) - the surface the broker supports.
