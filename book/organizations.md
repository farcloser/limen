# Creating and onboarding an organization

A new GitHub organization, from nothing to one whose repositories are born governed and
whose pull requests the bot can open: the steps in order, who takes each, and why it stands
where it does. The reasoning behind each setting is in [GitHub settings](./github.md) and
[identity and keys](./identity.md); this page is the order.

The order is the point. An organization's settings decide what every *new* repository is
born with, so they are set before the first repository exists, and the first repository is
the organization's own `.github`. Steps 1 to 7 are the human's, with an owner's token, a
browser and the machine, but for the invitation the bot accepts in step 2; its first pull
request is in step 8.

## The steps

1. **The human creates the organization**, and is its owner.

2. **The human brings the bot in, and the bot accepts.** The human creates a team named
   `agents` and adds the bot's account to it, which invites the bot to the organization in
   the same act:

   ```
   gh api -X POST orgs/<org>/teams -f name=agents -f privacy=closed
   gh api -X PUT orgs/<org>/teams/agents/memberships/<bot> -f role=member
   ```

   The bot's only access anywhere is what that team is granted
   ([the bot's identity](./identity.md#the-bots-identity)), and limen reports a missing or
   empty team but never creates or fills one: membership is a human's decision. Until the
   invitation is accepted the bot's membership reads `pending`, of the organization and of
   the team alike, and the team counts nobody. A session accepts it, as the bot, without
   being asked:

   ```
   gh api -X PATCH user/memberships/orgs/<org> -f state=active
   ```

   Both then read `active` (`gh api orgs/<org>/memberships/<bot>`). The team itself the
   bot's token cannot read: it answers 404, as the list of installed Apps does, so a
   session cannot confirm the next step and the human's check in step 4 does.

3. **The human installs Renovate on the organization**, for all repositories, so that one
   created later is covered without a second visit. It is a consent flow in a browser, and
   the one GitHub App the baseline depends on: without it no pin moves in any repository of
   the organization, silently.

4. **The human sets the organization's floor:** `limen github check -org <name>`, read,
   then `limen github fix -org <name>`, with an owner's token
   ([authentication](./github.md#authentication): the governed fields are absent from the
   answer to anyone else, and read as `unverifiable`). It is run from any directory at this
   point: the exceptions file it would read lives in a repository that does not exist yet.
   On an organization GitHub had just created (packcloser, 2026-11-26) the first check
   found five failures and the fix wrote all five: members creating public repositories
   (it turns member repository creation off altogether, the one form every plan has),
   members creating public Pages sites, the organization-wide sign-off, Actions allowed to
   all, and `uses:` not required to be pinned. The members' default permission, forking of
   private repositories, the workflow token and the approval of fork pull requests were
   already as the floor wants them. The agents team's grant has no repository to apply to
   yet. That run came after steps 2 and 3, and found the team and Renovate in place; the
   fix run before them was not tried.

5. **The human sets by hand what the fixer does not write.** After the fix the check still
   fails, on advisories: it exits non-zero on any of them, so each is done, here or in a
   later step, before the organization reads clean. In the organization's settings:
   - two-factor authentication required, once every member has it: turning it on removes
     the members who do not;
   - members may not change a repository's visibility, nor delete or transfer one: the API
     reads both and writes neither;
   - the profile's description, one line;
   - the policy on fine-grained personal access tokens: with none chosen the check of their
     grants reads `unverifiable`.

   The other advisories of that run are the next steps' own: the default code security
   configuration (step 6), and the owner roster and the `.github` repository (step 8).

6. **The human creates the default code security configuration**, with the owner's token:
   one request creates it, a second makes it the default for new repositories.

   ```
   id=$(gh api -X POST orgs/<org>/code-security/configurations \
     -f name=limen -f description='limen floor; Renovate is the one dependency bot' \
     -f advanced_security=enabled -f dependency_graph=enabled -f dependabot_alerts=enabled \
     -f dependabot_security_updates=disabled -f secret_scanning=enabled \
     -f secret_scanning_push_protection=enabled -f private_vulnerability_reporting=enabled \
     -f code_scanning_default_setup=not_set -f enforcement=enforced -q .id)
   gh api -X PUT "orgs/<org>/code-security/configurations/$id/defaults" -f default_for_new_repos=all
   ```

   Three of its fields are the ones that matter here:
   - `advanced_security=enabled` is what GitHub asks for beside secret scanning. Without
     the field, with `secret_protection=enabled` in its place, or with
     `advanced_security=secret_protection`, the request was refused on an organization of
     the free plan (2026-11-26): "GitHub advanced security must be enabled when any GHAS
     feature is set to enabled", HTTP 400.
   - `dependabot_security_updates=disabled`: Renovate is the one dependency bot, and a
     configuration that enables them overrides every repository's own setting. limen
     fails an organization that has one.
   - `code_scanning_default_setup=not_set` leaves code scanning to each repository, where
     it is an opt-in ([GitHub settings](./github.md)).

   limen checks that a default exists and does not create one. Without it, each repository
   created from here on is born unconfigured.

7. **The human tells the machine**, from a clone of limen-install at its latest release.
   The commands as they stand on this rig, the new organization's name the one thing added
   to the last:

   ```
   ./limen-install && ./limen-install --status
   taciturn key list -format ssh > "$TMPDIR/closer-claudio.pub"
   ./limen-install-agent --write ~/Library/Caches/ossein \
     --org duncanite --org farcloser --org forkcloser --org mycophonic --org packcloser \
     closer-claudio claudio@farcloser.world "$TMPDIR/closer-claudio.pub"
   ```

   - The first brings the machine's toolset to that release now, without waiting for the
     nightly run, and says whether the machine is where the release puts it. The `--org`
     option needs a release that has it.
   - The second writes the bot's public key as its agent prints it. The installer reads
     the file's first line and no other. Where taciturn holds more than one key, the bot's
     is the one every repository lists for it in `.lint-signers` with no `valid-before`,
     and this writes that one alone, from the limen-install clone itself:

     ```
     awk '$1 == "claudio@farcloser.world" && $2 !~ /valid-before/ { print $3, $4, "closer-claudio" }' \
       .lint-signers > "$TMPDIR/closer-claudio.pub"
     ```

   - The third names every organization and every `--write` directory, not the new one
     alone: both lists are the run's own, and what is not named again is dropped
     ([the bot's sandbox](./identity.md#the-bots-sandbox)). The list above is the rig's
     whole list; a new organization adds its name to it here, in the change that onboards
     it. The bot's own account is not in it: no session pushes to a repository there.

   Where sessions run under the command classifier, the `--org` list is what tells it the
   organization's repositories are the machine's own source control, so this comes before
   the bot's first push. The human's part of the next step does not wait for it.

8. **The organization's `.github` repository, its first.** Created as
   [any repository is](./github.md#creating-a-repository), in that order: the human creates
   it empty and **public** (GitHub ignores a private one as a source of community-health
   files) and runs `limen github fix -repo <name>/.github`, which grants the `agents` team
   write; the bot pushes the empty `Birth` commit that creates `main`; the bootstrap is the
   bot's first pull request; the human approves it and runs the fixer again. Beside what
   `limen bootstrap` seeds, that first pull request carries what makes it the
   organization's:
   - the community-health files, under `.github/`: `SECURITY.md`, `CONTRIBUTING.md` with
     the sign-off terms, and the rest of the set a sibling organization's `.github`
     carries;
   - the profile, `profile/README.md`;
   - `.lint-github.yaml`, with the owner roster, which is a declaration limen goes on
     checking and not an exemption:

     ```yaml
     org-admins: <login> is the sole owner of this organization
     ```

   - `.lint-signers`, copied from a sibling repository, as in every repository.

9. **The human runs the check again, from a checkout of that repository:** `limen github
   check -org <name>`. It reads the declarations there, and it is the first run that can
   come back clean. What it still reports is what is still missing.

10. **Each account watches the organization.** `limen github watch -org <name>` subscribes
   the account the token belongs to, so it is run twice: by a session, for the bot, and by
   the human, for the human's account ([watching an organization](./github.md#watching-an-organization)).
   It is run again whenever repositories have been added.

11. **limen's Renovate preset learns the name.** The shared preset names our organizations
    in four rules of `.limen/renovate.json`: the one that proposes an own release as soon as
    it exists, the two that keep own releases out of the third-party groups, and the one
    that reads a fork's rebuild tag. An organization missing from them has its releases held
    for the cooldown in every repository that consumes them, grouped with third parties, and
    a rebuild tag of its own never proposed. One change in limen, and the release that
    carries it.

12. **Repositories.** A new one is created as [any repository is](./github.md#creating-a-repository).
    One that exists elsewhere is transferred, which is the next section.

## Moving a repository in

A transfer changes a repository's owner, and every place that names the old one. What a
rename of seven repositories showed on 2026-11-26, and what follows from it for a transfer:

- **GitHub redirects most of the old name, not all of it.** The repository's page, its pull
  requests, its workflow runs and its release downloads answered with a redirect; a link to
  an issue under an old name answered 404, and the link check failed on it in every
  repository that cited it. Links are moved to the new name, the ones that redirect with the
  ones that do not.
- **A release is signed under the name it was cut under.** A registry entry that verifies a
  signer identity built from `owner/repo` goes on verifying the releases cut before the
  move, and fails the first one cut after it. The entry, the pin's name and the checksums
  move with that first release, not before and not after.
- **A Go module path is its own name.** Released versions keep the path they were tagged
  with; the move to a new path is a change of `go.mod` and of every import, in one commit,
  and Renovate does not propose it, since to it the new path is another module.
- **What the organization grants does not travel.** The `agents` team is the old
  organization's. After a transfer the human runs `limen github fix -repo <name>/<repo>`
  before the bot pushes, as for a new repository, and `limen github check -repo` says what
  else the move left behind.

A transfer between two of our organizations has not been done under this page yet. The
first one is the test of this section, and what it shows is written here.
