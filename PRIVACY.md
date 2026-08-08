# Privacy Policy

**Last Updated:** August 8, 2026

This Privacy Policy explains how **wtop** ("we", "us", or "our", maintained by [Michael Sanford](https://github.com/michaelsanford)) handles information in connection with the `wtop` software application and its associated GitHub repository and documentation.

We are committed to full compliance with global privacy and data protection standards, including:
- **GDPR / UK GDPR** (General Data Protection Regulation — European Union & United Kingdom)
- **PIPEDA** (Personal Information Protection and Electronic Documents Act — Canada Federal)
- **Loi 25** (Act to modernize legislative provisions as regards the protection of personal information — Quebec, Canada)
- **OAIC APPs** (Australian Privacy Principles under the Privacy Act 1988 — Australia)
- **CCPA / CPRA** (California Consumer Privacy Act / California Privacy Rights Act — California, United States)

---

## 1. Zero-Telemetry Guarantee (The Software)

**The `wtop` application does not collect, store, transmit, analyze, or monetize any personal data or telemetry.**

When you run `wtop.exe` on your computer:
- **No Analytics / Telemetry**: There is zero tracking code, telemetry collection, or remote analytics reporting.
- **Local Operation**: System metrics (CPU utilisation, physical memory, GPU usage, network I/O, process lists, and execution priority) are read strictly from local Windows operating system APIs (such as PDH, DXGI, and Win32 process APIs) directly in system RAM.
- **No Network Egress**: The application does not initiate outbound network connections to any external servers or telemetry endpoints.
- **Volatile Processing**: All metrics exist solely in transient memory for the purpose of rendering the terminal user interface (TUI) and are immediately discarded upon exiting the application.

---

## 2. Voluntary Information (GitHub Interactions)

The only personal or identifiable information collected in connection with `wtop` is information you **voluntarily provide** when interacting with the public GitHub repository ([`michaelsanford/wtop`](https://github.com/michaelsanford/wtop)):

- **Issue Reports & Discussions**: When you open an issue, feature request, or discussion, your public GitHub username, profile link, and any comments, system specifications, or diagnostic snippets you voluntarily paste into the issue body are recorded by GitHub.
- **Pull Requests & Git Commits**: When you submit a pull request, your Git author name, Git email address, and commit history are submitted to GitHub and become part of the project repository.

### Third-Party Platform Terms
GitHub's collection and hosting of repository data is governed by the [GitHub Privacy Statement](https://docs.github.com/en/site-policy/privacy-policies/github-privacy-statement).

---

## 3. Data Subject Rights & The Right to be Forgotten

We respect your statutory privacy rights across all applicable jurisdictions:

- **Right of Access & Portability**: You have the right to request information regarding any personal data we hold about you.
- **Right to Rectification**: You have the right to update or correct inaccurate personal data.
- **Right to Erasure ("Right to be Forgotten")**: You may request the deletion of voluntarily submitted personal data (such as issue descriptions, comments, or personal disclosures). You may also delete or edit your own issues and comments directly through GitHub at any time.
- **Right to Restriction / Objection**: You may object to or restrict the processing of your voluntary contributions.

### Open-Source Code Contribution Exception
> [!NOTE]
> In accordance with standard open-source governance and copyright licensing, once a pull request, code modification, or documentation patch has been accepted and merged into the project's codebase, the resulting Git commit history and code become an immutable part of the public repository licensed under the [MIT License](LICENSE). This perpetual license is necessary to preserve project integrity, reproducible builds, and dependency provenance for all downstream users.

---

## 4. Multi-Jurisdiction Compliance Disclosures

### A. European Union & United Kingdom (GDPR / UK GDPR)
- **Data Controller**: Michael Sanford (Contact via GitHub).
- **Legal Basis**: Legitimate interests (GDPR Art. 6(1)(f)) and explicit consent (GDPR Art. 6(1)(a)) for handling voluntarily submitted technical bug reports and feature requests.
- **Supervisory Authority**: You have the right to lodge a complaint with your local Data Protection Authority (DPA) or the UK Information Commissioner's Office (ICO).

### B. Canada (PIPEDA & Quebec Loi 25)
- **Accountability**: The project maintainer is responsible for personal information protection compliance under Canadian privacy legislation.
- **No Automated Profiling**: `wtop` performs no automated decision-making, profiling, biometric processing, or cross-site tracking.
- **Consent**: By submitting an issue or contribution on GitHub, you consent to the public processing of the information you provided for the explicit purpose of addressing your inquiry or contribution.

### C. Australia (Privacy Act 1988 & OAIC APPs)
- In accordance with the Australian Privacy Principles (APPs), we only collect personal information that is reasonably necessary for open-source collaboration, maintain appropriate safeguards, and provide individuals with access and correction rights upon request.

### D. California (CCPA / CPRA)
- **No Sale or Sharing of Personal Data**: We do not "sell" or "share" personal information as defined by California privacy laws. We have not sold or shared any consumer personal information in the preceding 12 months.
- **Non-Discrimination**: We will never discriminate against any user for exercising their privacy rights.

---

## 5. Security Safeguards

- No user credentials, authentication tokens, or personal identifiers are stored or managed by `wtop`.
- Release binaries are built in automated, isolated GitHub Actions runners, attested with cryptographic provenance ([GitHub Artifact Attestations](https://github.com/michaelsanford/wtop/attestations)), and signed with keyless Cosign signatures.

---

## 6. Changes to this Policy

We may update this Privacy Policy from time to time to reflect changes in legal requirements or project practices. Any updates will be published with a revised "Last Updated" date at the top of this document.

---

## 7. Contact Information

If you have any questions, concerns, or requests regarding this Privacy Policy or wish to exercise your right to erasure, please contact the maintainer:

- **GitHub Issues**: [Open an issue on GitHub](https://github.com/michaelsanford/wtop/issues)
- **Project Maintainer**: [@michaelsanford](https://github.com/michaelsanford)
