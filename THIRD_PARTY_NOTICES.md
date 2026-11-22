# Third-party notices

XGrowth Studio includes code from the following projects, distributed under the MIT License.
Their copyright and license notices are reproduced below, as the MIT License requires.

XGrowth Studio as a whole is distributed under the GNU Affero General Public License v3.0
(see `LICENSE`). The portions listed here remain available under their original MIT terms.

---

## Growth Studio

- Source: https://github.com/ai-timon-admin/growth-studio
- License: MIT

```
MIT License

Copyright (c) 2026 Timon

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

---

## AgentX

- Source: https://github.com/DezXBT/AgentX
- License: MIT

```
MIT License

Copyright (c) 2026 DezXBT

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

---

## Hermes Agent

- Source: https://github.com/NousResearch/hermes-agent (commit `05348a3`)
- License: MIT
- Elements adapted (each marked `adapté de hermes-agent : <path>@05348a3` in the code):
  - `tools/threat_patterns.py` (`INVISIBLE_CHARS`): set of invisible characters refused in learned rules, used in `performance.go` (`invisibleRune`, called by `ruleTextOK`).
  - `agent/background_review.py` (`_DO_NOT_CAPTURE_BLOCK`): idea of not turning one-off cases, failures of unknown cause or statements about how a tool works into lasting rules, rewritten in French in the performance analysis prompt, `performance.go` (`perfSystem`).

```
MIT License

Copyright (c) 2025 Nous Research

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

---

## linkedin-skills

- Source: https://github.com/sergebulaev/linkedin-skills (commit `32083c0`)
- License: MIT
- Elements adapted (rewritten in French as Go string constants, no file copied, none of its scripts called; each marked `adapté de linkedin-skills@32083c0` in the code), used in `linkedin_method.go`:
  - `skills/linkedin-post-writer` (hooks: standalone first line within 210 characters, no question or "here's how" opener): `liHooksMethod`.
  - `skills/linkedin-humanizer` (humanizer and its AI-tells lists in `references/audit-ai-tells.md`; post audit in `sub-skills/post-audit.md`): `liHumanizerMethod`, `liAuditMethod` and the patterns of the deterministic detector `TicCheck`.
  - `skills/linkedin-content-planner` (7-day plan): `liPlanMethod`.
  - Always bent to the voice of the account (the voice of the account prevails over the method).

```
MIT License

Copyright (c) 2026 Sergey Bulaev

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

---

## Inspiration only (inspiration seulement)

- **OpenBot** (PolyForm Noncommercial license): its approach to agent memory and self-improvement was a conceptual inspiration for the learned rules of XGrowth Studio. No code, prompt or schema from OpenBot is included, copied or translated in XGrowth Studio.
