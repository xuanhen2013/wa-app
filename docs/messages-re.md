# WA message display reverse notes

## Message oneof

The decrypted native payload is the WhatsApp `Message` proto:

- `X/C30285DcY.java`: top-level message oneof.
- `conversation = 1`, `extended_text = 6`, `buttons = 42`, `buttons_response = 43`, `interactive = 45`, `interactive_response = 48`.

## Rich text surfaces

Display text should be assembled from typed message subfields instead of showing raw proto/JSON blobs:

- `X/C30277DcQ.java` (`extended_text`): `text = 1`, `matched_text = 2`, `description = 5`, `title = 6`.
- `X/C8PM.java` (`buttons_message`): `text = 1`, `content_text = 6`, `footer_text = 7`, repeated `buttons = 9`.
- `X/C8P7.java`, `X/DZS.java`, `X/DZT.java`, `X/DZR.java`: quick-reply / URL / call buttons carry `display_text = 1`; URL/call payload is field `2`.
- `X/C8PN.java` (`list_message`): `title = 1`, `description = 2`, `button_text = 3`, `footer_text = 7`.
- `X/C30206DbG.java` and `X/C30209DbJ.java` (`list_response`): reply title/description plus selected display text.
- `X/C8PZ.java` (`interactive_message`): `header = 1`, `body = 2`, `footer = 3`, `native_flow = 6`, `carousel = 7`, `bloks_widget = 8`.
- `X/C187078Oc.java`: interactive body/footer text is nested at field `1`.
- `X/C8PX.java`: interactive header title/subtitle are fields `1` and `2`.
- `X/C187308Oz.java`, `X/C187198Oo.java`: native-flow button `name = 1`, `button_params_json = 2`, message params JSON field `2`.
- `X/C8PC.java`: bloks widget `data = 2`, `fallback = 4`.
- `X/C30164Daa.java`, `X/C30146DaI.java`: interactive response body and native-flow response name/params JSON.

JSON strings in rich-message params may contain keys such as `display_text`, `url`, `title`, `body`, and `description`; wa-app normalizes these into line-based display text and lets the frontend render links.

## One-time historical plaintext backfill

Historical rows created before `plaintext_value` was persisted can be repaired by re-invoking `WaExtractionService.DecryptMessage` with `include_sensitive_plaintext = true` and `SESSION_COMMIT_POLICY_TRANSIENT`. This is an operational one-off: it writes normal `wa_decrypted_messages` rows through the service path and does not add migration code or retain temporary tooling.
