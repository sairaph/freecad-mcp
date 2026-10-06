# Example scripts

[Back to README](../README.md) · [Installation](installation.md) · [Tools](tools.md)

These are examples, not tested in CI.

| Example | Description |
| --- | --- |
| [Cantilever FEM analysis](../examples/cantilever_fem.py) | Build a cantilever, run CalculiX, and compare the results with an analytical solution. |
| [Google ADK agent](../examples/adk/agent.py) | Connect an ADK agent to the installed `freecad-mcp` binary. |
| [LangChain / LangGraph agent](../examples/langchain/react.py) | Run an interactive CAD agent using MCP tools and a Groq model. |

The agent examples use optional third-party dependencies and provider
configuration. Install `freecad-mcp` first (see the [README](../README.md#install))
and adjust the model settings in each example before running it; its model name
(`gemini-2.5-flash-lite` for ADK, `llama-3.1-8b-instant` for LangChain) is a
placeholder, not a recommendation. The ADK
example reads `GOOGLE_API_KEY` from [`examples/adk/.env`](../examples/adk/.env);
replace its placeholder value with your key. The LangChain example expects
`GROQ_API_KEY` in the environment.
