# WA contacts reverse notes

## Local stores

The Android app keeps two contact projections in `wa.db`:

- `wa_contacts`: WhatsApp-owned contact projection keyed by `jid`.
- `wa_address_book`: Android address-book projection enriched with WhatsApp status and JID mapping.

The app joins both projections with profile/group metadata tables when rendering or syncing contacts:

- `wa_vnames` for verified business names.
- `wa_group_descriptions` for group descriptions.
- `wa_group_admin_settings` for group flags.
- `wa_biz_profiles` for business profile metadata.

## Main app queries

Observed JADX references:

- `X/C0RT.java` and `X/C0RV.java`: read `wa_contacts` by JID, JID set, JID pattern, and phone number.
- `X/C37C.java`: reads `wa_address_book` for contact picker, sync upload batches, and native contacts.
- `X/C06530Sy.java`: persists address-book contacts and lists all DB contacts for sync.
- `X/C65822uV.java`: builds the address-book picker SQL.
- `X/AnonymousClass846.java`: builds `usync` IQ requests for phone/JID/LID/username contact lookups.

The minimal UI-safe field set for wa-app is therefore:

- `jid`
- `number`
- `display_name`
- `wa_name`
- `verified_name`
- `is_whatsapp_user`
- `is_reachable`
- derived `kind`

Raw Android contact IDs, table names, DB paths, and WA internal sync protocol flags stay internal to extractors and are not exposed through public proto.

## App-side filtering worth preserving

The app's WhatsApp-user contact query filters out special JIDs:

- `broadcast`
- `*@broadcast`
- `*@g.us` for user-only lists
- `*@temp`
- `*@newsletter`
- self JID

Address-book picker queries order by `display_name`, `jid`, and `phone_type`.

## LID to phone-number mapping

Observed reverse points from `workspace/wa-eng/app-release-re`:

- Main WA message proto `X/C30285DcY.java` uses `PROTOCOL_MESSAGE_FIELD_NUMBER = 12`.
- Protocol message `X/C30281DcU.java` uses `LID_MIGRATION_MAPPING_SYNC_MESSAGE_FIELD_NUMBER = 23`.
- `X/DYF.java` contains `encodedMappingPayload` at field `1`.
- `SendLidMigrationMappingSyncJob.java` shows this payload is gzip-compressed `X/C30110DZi`.
- `X/C30110DZi.java` has repeated `pnToLidMappings` at field `1` and `chatDbMigrationTimestamp` at field `2`.
- `X/C30165Dab.java` mapping entries use numeric `pn = 1`, `assigned_lid = 2`, and `latest_lid = 3`.
- History sync `X/C24912B0t.java` additionally contains `phoneNumberToLidMappings = 15` (`X/B03.java`: `pn_jid = 1`, `lid_jid = 2`) and `inlineContacts = 20` (`X/C24896B0d.java`: `pn_jid = 1`, `lid_jid = 2`, `full_name = 3`, `first_name = 4`, `username = 5`).
- Incoming chat nodes may carry `notify`, `display_name`, `sender_lid`, `sender_pn`, `participant_lid`, `participant_pn`, `peer_recipient_lid`, `peer_recipient_pn`, `recipient_latest_lid`, `username`, `peer_recipient_username`, and `participant_username` attrs; these are treated as opportunistic contact hints only.
- More LID aliases are surfaced outside the encrypted message body. The app stores username/name hints from:
  - broadcast recipients: `participants/to@peer_recipient_lid`, `peer_recipient_pn`, `peer_recipient_username` (`X/C164577Rd.java`);
  - status receipts: `participant`, `participant_pn`, `participant_username` (`X/C1Np.java`);
  - group notifications: `participant` + `participant_username`, child `author` + `author_username` (`X/C16240oO.java`).
  - incoming message stanza LID parser: `sender_lid`/`sender_pn`, `participant_lid`/`participant_pn`, `peer_recipient_lid`/`peer_recipient_pn`, `recipient_latest_lid`, `peer_recipient_username`, and `participant_username` (`X/C7SJ.java`);
  - peer and creator PN attrs used by protocol parsers: `peer_pn_jid` (`X/A3X.java`), `creator_pn` and `participant_pn` (`X/C214599hw.java`), `contact_pn` (`X/C22573A0j.java`), and inactive notification `sender_pn_jid` (`X/C23628Acm.java`, `X/C23627Acl.java`).
  wa-app now scans incoming chat-node attributes structurally, stores safe LID/PN/name/username hints at the native state level, and also keeps per-payload hints beside encrypted payload metadata. This matters because some useful alias updates arrive on non-message or plaintext notification nodes and would otherwise be lost before a later contact resolve.
- The local app also has durable mapping/display tables separate from the normal contact rows:
  - `jid_map` maps PN row IDs to LID row IDs and is read by `WaJidMapRepository` (`X/C06080Re.java`, `X/C0TE.java`).
  - `lid_display_name(lid_row_id, display_name, username)` carries display data for LID-only contacts (`X/C0LF.java`, `X/B3V.java`).
  - `contact_metadata(contact_lid, contact_pn, contact_username, contact_push_name)` is another non-message source for LID/PN/name hints (`X/C22573A0j.java`).
  wa-app does not expose these storage details in proto; it only projects the resolved account-local contact identity.
- App-state contact actions carry local contact display data and LID/PN pairs:
  - `contact_action` (`X/C24903B0k.java`): `full_name = 1`, `first_name = 2`, `lid_jid = 3`, `pn_jid = 5`, `username = 6`.
  - `pn_for_lid_chat_action` (`X/C2OM.java`): `pn_jid = 1`; the enclosing app-state key is required before it can be used as a mapping.
  - `lid_contact_action` (`X/B0O.java`): `full_name = 1`, `first_name = 2`, `username = 3`; this record has display data only and must not be treated as a LID/PN mapping by itself.
  - `out_contact_action` (`X/C44534Jpe.java`): `full_name = 1`, `first_name = 2`; the enclosing app-state key supplies the LID/PN side of the contact identity.
- App-state encrypted mutation values unwrap through `X/C44404JnF.java`:
  - `index = 1` stores the JSON key array built by `X/AbstractC61462n5.java`.
  - `value = 2` stores the `X/C2P0.java` app-state value.
  - Contact keys are shaped like `["contact", "<pn-jid>"]`; `X/C23591AcA.java` puts the paired LID in `contact_action.lid_jid`.
  - LID chat mapping keys are shaped like `["pnForLidChat", "<lid-jid>"]`; the PN is in `pn_for_lid_chat_action.pn_jid`.
  - LID display-name keys are shaped like `["lid_contact", "<lid-jid>"]`; display data is in `lid_contact_action`.
  - Out-contact keys use the same indexed app-state wrapper and carry display data through `out_contact_action`.

wa-app now extracts these as internal contact hints and projects them into the account-local `WAContact` record keyed by the LID JID. The public contract is unchanged; the UI should show the resolved phone-number/name when available and never surface the raw LID as the display title.

## 2026-06-08 active usync contact query

Reverse target: `AnonymousClass846.A0E(querySyncUsernameByLid)` and `C08180Zr.A05/A06/BuX`.

Findings:
- LID lookup uses a normal usync IQ, not a message body migration payload:
  - `iq`: `xmlns="usync"`, `type="get"`, generated `id`.
  - `usync`: `sid=sync_sid_query*`, `index="0"`, `last="true"`, `mode="query"`, `context="interactive"`.
  - `query`: request protocols include `username`, `contact`; `lid`/`business` are safe extra protocols for PN and verified-name enrichment.
  - `list/user`: request users carry `jid=<lid-jid>`.
  - App order is `query` first, then `list`; keep this order when probing because some servers return only echoed LID rows for non-canonical query shapes.
- Response parser in `C08180Zr.BuX` reads `usync/list/user` and `usync/side_list/user`:
  - `user@jid` is the primary jid; for LID requests this is expected to be `*@lid`.
  - `user@pn_jid`/`new_jid` provide PN mapping.
  - child `lid@val` may provide a LID when response is PN-centered.
  - child `username`, nested `username/username_info@username`, and `contact@username` provide username.
  - child `business@pn_jid` and `business/verified_name` can enrich PN and verified-name.
- Binary node token table must follow `AbstractC34581eJ.A00`; older partial token maps shift `mode/query/list/lid/usync/side_list/error` and make usync requests unreadable.

Implementation note:
- wa-app now has a bounded active resolver (`ResolveWAContacts`) that queries unresolved `*@lid` contacts through chatd usync and upserts only recovered contact projections. It does not log JIDs, names, phone numbers, message bodies, or reusable session material.

Follow-up:
- The first active query in the sandbox returned structurally valid `user` rows but no PN/name enrichment. To keep the reverse loop safe, the resolver now emits only protocol-shape logs: variant name, IQ type, user counts, attr/tag keys, JID domain classes, and PN/LID/name hint counts. It never prints raw JIDs, phone numbers, names, OTPs, message bodies, tokens, or session material.
- The resolver tries bounded protocol variants until the whole batch is enriched instead of treating echoed LID rows as resolved identities:
  - app-exact `interactive/query` with `username` + `contact`;
  - richer `interactive/query` with `username` + `contact` + `lid` + `business`;
  - `interactive/query` with `contact@addressing_mode=lid`, `lid`, `username`, and `business`;
  - LID-migration `message/query` and `notification/query` with `contact@addressing_mode=lid`;
  - `interactive` side-list probes with `sidelist@addressing_mode=lid` and with app-observed plain `sidelist`.
- `resolved_count` now counts contacts that actually gained a phone number, WA username, verified name, or non-fallback display name. Echoed `*@lid` rows without enrichment remain contacts but are not counted as resolved.
- Active usync is only an enrichment path. Network/proxy/dial/timeout failures must not make the contacts UI fail: wa-app returns the existing local projection and `queried_count` while leaving `resolved_count` unchanged. Non-retryable protocol-shape errors remain surfaced so reverse regressions are still visible.
