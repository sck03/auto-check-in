# Third-party notices

The Go and Vue implementation in this project is independently authored. No code or UI assets from Fl API Hub are included. Its architecture and product requirements were reviewed as design references:

- RebornQ/fl_api_hub, AGPL-3.0, revision 94ef25b0381d54ab11d1631d569a5cca4451eb84.
- QuantumNous/new-api, AGPL-3.0, revision 19d435e7fdfdaa2b9a734a76393f112cf980b815. OAuth and authentication protocols were reviewed for interoperability; no implementation or UI assets are included.
- suainam/anyrouter-autolog, BSD-2-Clause, revision 687aa92944d19e83bc9c1cc25e0b544b4fa19525.
- millylee/anyrouter-check-in, BSD-2-Clause, revision 3b16662142c68703ea3e79c77c06a8cf6bba7990.
- aceHubert/newapi-ai-check-in, BSD-2-Clause, revision a01c232672686ce66ac06194a7f13b04ca734a28. WONG and x666 daily reward protocols were reviewed; the shared Milly BSD notice is retained below.
- Jasonliu-0/Newapi-checkin, MIT, revision 49089a5bbe4c063fdb814e77d27168e55bed1700. Session reuse, error reporting and New API protocols were reviewed; the Go implementation is independently authored.

Protocol compatibility and behavior were informed by the two BSD-licensed projects. Their notice is retained below. The project itself has not yet selected an open-source license.

Release archives include a single `THIRD_PARTY_NOTICES.md` file. At build time, `scripts/licenses.mjs` appends the component versions and complete license/notice texts for Go, Go modules and Vue runtime dependencies to this source notice. Full project documentation stays in the repository. Browsers installed on the user's operating system are not bundled in Windows archives. The Docker image retains Debian's Chromium package and its distribution-provided notices, plus the same consolidated application notice.

## BSD 2-Clause License

Copyright (c) 2025, Milly

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are met:

1. Redistributions of source code must retain the above copyright notice, this
   list of conditions and the following disclaimer.

2. Redistributions in binary form must reproduce the above copyright notice,
   this list of conditions and the following disclaimer in the documentation
   and/or other materials provided with the distribution.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS"
AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE LIABLE
FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR
SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER
CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY,
OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.

## MIT License — NewAPI-Checkin reference

Copyright (c) 2026 NewAPI-Checkin Contributors

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
