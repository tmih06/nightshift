# The software factory, defined

Research note, November 2026. Question: what is a software factory?
The short answer: nobody owns the term. Since 1968 it has carried five
different definitions, and each one kept the factory metaphor while
swapping what counts as the product, the workers, and the assembly line.

## The five definitions at a glance

| Years | Definition owner | A software factory is |
|---|---|---|
| 1968 | R. W. Bemer, General Electric | A machine-controlled production environment where all programming happens inside its tooling |
| 1969 to 1980s | Hitachi, SDC, NEC, Toshiba, Fujitsu | An organization that runs many projects through standardized process, reuse, and quality control |
| 2004 | Greenfield and Short, Microsoft | A product-line template that configures an IDE into a factory for one family of applications |
| 2022 | CNCF TAG Security | The set of systems that turns source code into signed, verifiable artifacts |
| 2026 | Practitioners and analysts | An automation loop that runs agents across triage, spec, implementation, review, and shipping |

## 1968, the production environment

The term appeared at the NATO Software Engineering Conference in Garmisch,
the same conference that coined "software engineering" itself. Robert W.
Bemer of General Electric proposed a "machine-controlled production
environment" and gave the first working definition. Cusumano, the term's
historian, credits that 1968 paper as "the industry's first working
definition of what might constitute a software factory".

Bemer's definition, from the conference report:

> Program construction, checkout and usage are done entirely within this
> environment using the tools it contains. Ideally it should be impossible
> to produce programs exterior to this environment.

His goals were to maximize programmer effectiveness, minimize time and
cost, and maintain quality, through standardized tools, a computer-based
interface, and a historical database for financial and management
controls.

At the same conference, M. D. McIlroy of AT&T supplied the other half of
the metaphor, mass-produced parts. He asked for "standard catalogues of
routines" and closed with:

> What I have just asked for is simply industrialism, with programming
> terms substituted for some of the more mechanically oriented terms
> appropriate to mass production.

## 1969 to the 1980s, the organization

Hitachi was the first company to name a real facility after the idea. By
founding its Software Works in 1969, Hitachi "was the first company in the
world to apply the term factory (actually, its Japanese equivalent, kojo,
translated either as 'factory' or 'works') to an actual software facility".
The intent was explicit:

> The factory represented a deliberate attempt to transform software from
> an unstructured "service" to a "product" with a guaranteed level of cost
> and quality.

System Development Corporation followed in 1975, then NEC, Toshiba, and
Fujitsu around 1976 and 1977. Cusumano characterizes this whole generation
as factory-like approaches that emphasize "standardization of development
methods and tools, systematic reuse of program components or designs,
some divisions of labor and functional departments, and disciplined
project management as well as product quality control". The story he tells
is a move "beyond loosely organized craft or job-shop modes of operation
that treated each project as unique to a more structured process and
organization for multiple projects".

## 2004, the product line

Jack Greenfield and Keith Short of Microsoft revived the term for
model-driven development. Their 2004 Wiley book, "Software Factories:
Assembling Applications with Patterns, Models, Frameworks, and Tools",
defined a software factory as the confluence of component-based
development, model-driven development, and software product lines. The
OOPSLA 2003 paper puts the definition in one step:

> An IDE configured with a software template for a product family becomes
> a factory for producing members of the family. This is what we call a
> software factory.

Here the factory is not an organization or a building. It is a packaged
template, a schema plus patterns, models, frameworks, and tools, loaded
into a development environment so that one family of applications gets
assembled rather than hand-written. The MSDN pages behind this work gave
the definition Wikipedia still leads with, a software factory as "a
structured collection of related software assets" that produces software
"through an assembly process".

## 2022, the supply chain

CNCF TAG Security adopted the term for supply-chain security in its Secure
Software Factory reference architecture: "This reference architecture
adopts the 'Software Factory' model for designing a secure software supply
chain." The factory here is the build system itself, five entities,
source code, materials, build pipelines, artifacts, and deployments,
organized into core, management, and distribution components. Its outputs
are not programs but signed artifacts with provenance, SBOMs, and
attestations.

A finding on the citation chain: the paper's footnote for the "Software
Factory" model points to the Wikipedia article, and Wikipedia's definition
traces back to the 2004 Greenfield and Short book and its MSDN pages. The
2022 meaning and the 2004 meaning sit in one unbroken reference chain.

## 2026, the agent loop

Current usage, from practitioner writing and vendor marketing rather than
a primary standard. Warp defines it as "a cloud-based automation loop
around the software development lifecycle, triage, spec, implementation,
review, verification, shipping". Factory.com calls it "a managed model for
producing software with repeatable inputs, standardized tooling,
measurable output, and enough retained context to recover state". Gartner
sells a 2026 "Innovation Insight" report on AI software factories, which I
have not read, so its exact definition is known here only through vendor
announcements that name the report.

## What every definition keeps

All five generations hold on to the same four factory moves.

1. A controlled environment. Production happens inside it. Bemer wanted it
   to be impossible to produce programs outside the environment, and CNCF's
   admission control polices what enters the build.
2. Standardized tools and process, in place of individual craftsmanship.
3. Systematic reuse of parts, from McIlroy's component catalogues to
   product-line assets, base images, and warm sandboxes.
4. Measurement and quality control on the output, from Hitachi's
   guaranteed cost and quality to provenance and attestations.

What changes between generations is the product, a program, a member of an
application family, a signed artifact, a shipped change, and the worker, a
programmer, an assembler, a pipeline, or an agent.

## What this means for this repo

The demo in this repo is a small software factory by the oldest definition
and the newest. The `SandboxTemplate` is the standardized environment and
tooling, the `SandboxWarmPool` is standing capacity, and the `SandboxClaim`
is the work order that consumes it. Deleting the claim tears the product
down and the pool restocks, so the factory converges to the same state.
Bemer's 1968 sentence is the design rule the warm pool enforces for agent
workloads: the work happens inside the controlled environment, never
outside it.

## Sources

- Bemer, R. W. "The Software Factory, Concept and Compact History", [archived
  page](https://web.archive.org/web/20090215121109/http://trailing-edge.com/~bobbemer/FACTORY.HTM),
  quotes the 1968 NATO conference report verbatim.
- Cusumano, M. A. "The Software Factory: An Entry for the Encyclopedia of
  Software Engineering", MIT Sloan WP#BPS-3268-91, [draft
  PDF](http://gregorystrachta.com/resources/Touchstones/swp-3268-23661042.pdf),
  March 1991. Also "The Software Factory: A Historical Interpretation",
  IEEE Software 6(2), March 1989, paywalled.
- Greenfield, J. and Short, K. "Software Factories: Assembling Applications
  with Patterns, Models, Frameworks and Tools", [OOPSLA 2003 companion
  paper](https://s23m.com/oopsla2003/greenfield.pdf) and the [Wiley
  2004 book](https://www.softwarefactories.com/TheBook.html).
- CNCF TAG Security. "Secure Software Factory" [reference
  architecture](https://tag-security.cncf.io/community/working-groups/supply-chain-security/secure-software-factory/secure-software-factory/),
  2022.
- Wikipedia. "Software factory", [article](https://en.wikipedia.org/wiki/Software_factory),
  used to trace the MSDN and book citations.
- Warp. "What Is a Software Factory?", [article](https://www.warp.dev/articles/what-is-a-software-factory),
  September 2026, practitioner definition.
- Factory. "Inside the software factory", [article](https://factory.com/articles/what-is-a-software-factory),
  July 2026, practitioner definition.
