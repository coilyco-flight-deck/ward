# ward-kdl ops trello

Spec-driven CLI. Every verb issues an HTTP request against the API base https://api.trello.com/1.

Authenticates with query parameters (scheme query-param), reading each secret from key=ssm /trello/api-key, token=ssm /trello/api-token. The secret values are never shown.

## ward-kdl ops trello board get

`GET /boards/{id}`

Authorized by grant: can get board. Not destructive.

Positional arguments (1):

- `<id>` (string)

Options (16):

- `--actions` (string, optional): This is a nested resource. Read more about actions as nested resources [here](/cloud/trello/guides/rest-api/nested-resources/).
- `--boardStars` (string, optional): Valid values are one of: `mine` or `none`.
- `--cards` (string, optional): This is a nested resource. Read more about cards as nested resources [here](/cloud/trello/guides/rest-api/nested-resources/).
- `--card_pluginData` (boolean, optional): Use with the `cards` param to include card pluginData with the response
- `--checklists` (string, optional): This is a nested resource. Read more about checklists as nested resources [here](/cloud/trello/guides/rest-api/nested-resources/).
- `--customFields` (boolean, optional): This is a nested resource. Read more about custom fields as nested resources [here](#custom-fields-nested-resource).
- `--fields` (string, optional): The fields of the board to be included in the response. Valid values: all or a comma-separated list of: closed, dateLastActivity, dateLastView, desc, descData, idMemberCreator, idOrganization, invitations, invited, labelNames, memberships, name, pinned, powerUps, prefs, shortLink, shortUrl, starred, subscribed, url
- `--labels` (string, optional): This is a nested resource. Read more about labels as nested resources [here](/cloud/trello/guides/rest-api/nested-resources/).
- `--lists` (string, optional): This is a nested resource. Read more about lists as nested resources [here](/cloud/trello/guides/rest-api/nested-resources/).
- `--members` (string, optional): This is a nested resource. Read more about members as nested resources [here](/cloud/trello/guides/rest-api/nested-resources/).
- `--memberships` (string, optional): This is a nested resource. Read more about memberships as nested resources [here](/cloud/trello/guides/rest-api/nested-resources/).
- `--pluginData` (boolean, optional): Determines whether the pluginData for this board should be returned. Valid values: true or false.
- `--organization` (boolean, optional): This is a nested resource. Read more about organizations as nested resources [here](/cloud/trello/guides/rest-api/nested-resources/).
- `--organization_pluginData` (boolean, optional): Use with the `organization` param to include organization pluginData with the response
- `--myPrefs` (boolean, optional)
- `--tags` (boolean, optional): Also known as collections, tags, refer to the collection(s) that a Board belongs to.

## ward-kdl ops trello board create

`POST /boards/`

Authorized by grant: can create board. Not destructive.

Options (16):

- `--name` (string, required): The new name for the board. 1 to 16384 characters long.
- `--defaultLabels` (boolean, optional): Determines whether to use the default set of labels.
- `--defaultLists` (boolean, optional): Determines whether to add the default set of lists to a board (To Do, Doing, Done). It is ignored if `idBoardSource` is provided.
- `--desc` (string, optional): A new description for the board, 0 to 16384 characters long
- `--idOrganization` (string, optional): The id or name of the Workspace the board should belong to.
- `--idBoardSource` (string, optional): The id of a board to copy into the new board.
- `--keepFromSource` (string, optional): To keep cards from the original board pass in the value `cards`
- `--powerUps` (string, optional): The Power-Ups that should be enabled on the new board. One of: `all`, `calendar`, `cardAging`, `recap`, `voting`.
- `--prefs_permissionLevel` (string, optional): The permissions level of the board. One of: `org`, `private`, `public`.
- `--prefs_voting` (string, optional): Who can vote on this board. One of `disabled`, `members`, `observers`, `org`, `public`.
- `--prefs_comments` (string, optional): Who can comment on cards on this board. One of: `disabled`, `members`, `observers`, `org`, `public`.
- `--prefs_invitations` (string, optional): Determines what types of members can invite users to join. One of: `admins`, `members`.
- `--prefs_selfJoin` (boolean, optional): Determines whether users can join the boards themselves or whether they have to be invited.
- `--prefs_cardCovers` (boolean, optional): Determines whether card covers are enabled.
- `--prefs_background` (string, optional): The id of a custom background or one of: `blue`, `orange`, `green`, `red`, `purple`, `pink`, `lime`, `sky`, `grey`.
- `--prefs_cardAging` (string, optional): Determines the type of card aging that should take place on the board if card aging is enabled. One of: `pirate`, `regular`.

## ward-kdl ops trello board edit

`PUT /boards/{id}`

Authorized by grant: can edit board. Not destructive.

Positional arguments (1):

- `<id>` (string)

Options (15):

- `--name` (string, optional): The new name for the board. 1 to 16384 characters long.
- `--desc` (string, optional): A new description for the board, 0 to 16384 characters long
- `--closed` (boolean, optional): Whether the board is closed
- `--subscribed` (string, optional): Whether the acting user is subscribed to the board
- `--idOrganization` (string, optional): The id of the Workspace the board should be moved to
- `--prefs/permissionLevel` (string, optional): One of: org, private, public
- `--prefs/selfJoin` (boolean, optional): Whether Workspace members can join the board themselves
- `--prefs/cardCovers` (boolean, optional): Whether card covers should be displayed on this board
- `--prefs/hideVotes` (boolean, optional): Determines whether the Voting Power-Up should hide who voted on cards or not.
- `--prefs/invitations` (string, optional): Who can invite people to this board. One of: admins, members
- `--prefs/voting` (string, optional): Who can vote on this board. One of disabled, members, observers, org, public
- `--prefs/comments` (string, optional): Who can comment on cards on this board. One of: disabled, members, observers, org, public
- `--prefs/background` (string, optional): The id of a custom background or one of: blue, orange, green, red, purple, pink, lime, sky, grey
- `--prefs/cardAging` (string, optional): One of: pirate, regular
- `--prefs/calendarFeedEnabled` (boolean, optional): Determines whether the calendar feed is enabled or not.

## ward-kdl ops trello board list-lists - read a board's lists sub-collection

`GET /boards/{id}/lists`

Authorized by grant: can list-lists board. Not destructive.

Positional arguments (1):

- `<id>` (string)

Options (4):

- `--cards` (string, optional): Filter to apply to Cards.
- `--card_fields` (string, optional): `all` or a comma-separated list of card [fields](/cloud/trello/guides/rest-api/object-definitions/#card-object)
- `--filter` (string, optional): Filter to apply to Lists
- `--fields` (string, optional): `all` or a comma-separated list of list [fields](/cloud/trello/guides/rest-api/object-definitions/)

## ward-kdl ops trello board list-cards - read a board's cards sub-collection

`GET /boards/{id}/cards`

Authorized by grant: can list-cards board. Not destructive.

Positional arguments (1):

- `<id>` (string)

## ward-kdl ops trello list get

`GET /lists/{id}`

Authorized by grant: can get list. Not destructive.

Positional arguments (1):

- `<id>` (string)

Options (1):

- `--fields` (string, optional): `all` or a comma separated list of List field names.

## ward-kdl ops trello list create

`POST /lists`

Authorized by grant: can create list. Not destructive.

Options (3):

- `--name` (string, required): Name for the list
- `--idBoard` (string, required): The long ID of the board the list should be created on
- `--idListSource` (string, optional): ID of the List to copy into the new List

## ward-kdl ops trello list create-on-board

`POST /boards/{id}/lists`

Authorized by grant: can create-on-board list. Not destructive.

Positional arguments (1):

- `<id>` (string)

Options (2):

- `--name` (string, required): The name of the list to be created. 1 to 16384 characters long.
- `--pos` (string, optional): Determines the position of the list. Valid values: `top`, `bottom`, or a positive number.

## ward-kdl ops trello card get

`GET /cards/{id}`

Authorized by grant: can get card. Not destructive.

Positional arguments (1):

- `<id>` (string)

Options (18):

- `--fields` (string, optional): `all` or a comma-separated list of [fields](/cloud/trello/guides/rest-api/object-definitions/). **Defaults**: `badges, checkItemStates, closed, dateLastActivity, desc, descData, due, start, idBoard, idChecklists, idLabels, idList, idMembers, idShort, idAttachmentCover, manualCoverAttachment, labels, name, pos, shortUrl, url`
- `--actions` (string, optional): See the [Actions Nested Resource](/cloud/trello/guides/rest-api/nested-resources/#actions-nested-resource)
- `--attachments` (string, optional): `true`, `false`, or `cover`
- `--attachment_fields` (string, optional): `all` or a comma-separated list of attachment [fields](/cloud/trello/guides/rest-api/object-definitions/)
- `--members` (boolean, optional): Whether to return member objects for members on the card
- `--member_fields` (string, optional): `all` or a comma-separated list of member [fields](/cloud/trello/guides/rest-api/object-definitions/). **Defaults**: `avatarHash, fullName, initials, username`
- `--membersVoted` (boolean, optional): Whether to return member objects for members who voted on the card
- `--memberVoted_fields` (string, optional): `all` or a comma-separated list of member [fields](/cloud/trello/guides/rest-api/object-definitions/). **Defaults**: `avatarHash, fullName, initials, username`
- `--checkItemStates` (boolean, optional)
- `--checklists` (string, optional): Whether to return the checklists on the card. `all` or `none`
- `--checklist_fields` (string, optional): `all` or a comma-separated list of `idBoard,idCard,name,pos`
- `--board` (boolean, optional): Whether to return the board object the card is on
- `--board_fields` (string, optional): `all` or a comma-separated list of board [fields](/cloud/trello/guides/rest-api/object-definitions/#board-object). **Defaults**: `name, desc, descData, closed, idOrganization, pinned, url, prefs`
- `--list` (boolean, optional): See the [Lists Nested Resource](/cloud/trello/guides/rest-api/nested-resources/)
- `--pluginData` (boolean, optional): Whether to include pluginData on the card with the response
- `--stickers` (boolean, optional): Whether to include sticker models with the response
- `--sticker_fields` (string, optional): `all` or a comma-separated list of sticker [fields](/cloud/trello/guides/rest-api/object-definitions/)
- `--customFieldItems` (boolean, optional): Whether to include the customFieldItems

## ward-kdl ops trello card create

`POST /cards`

Authorized by grant: can create card. Not destructive.

Options (15):

- `--name` (string, optional): The name for the card
- `--desc` (string, optional): The description for the card
- `--due` (string, optional): A due date for the card
- `--start` (string, optional): The start date of a card, or `null`
- `--dueComplete` (boolean, optional): Whether the status of the card is complete
- `--idList` (string, required): The ID of the list the card should be created in
- `--urlSource` (string, optional): A URL starting with `http://` or `https://`. The URL will be attached to the card upon creation.
- `--fileSource` (string, optional)
- `--mimeType` (string, optional): The mimeType of the attachment. Max length 256
- `--idCardSource` (string, optional): The ID of a card to copy into the new card
- `--keepFromSource` (string, optional): If using `idCardSource` you can specify which properties to copy over. `all` or comma-separated list of: `attachments,checklists,customFields,comments,due,start,labels,members,start,stickers`
- `--address` (string, optional): For use with/by the Map View
- `--locationName` (string, optional): For use with/by the Map View
- `--coordinates` (string, optional): For use with/by the Map View. Should take the form latitude,longitude
- `--cardRole` (string, optional): For displaying cards in different ways based on the card name. Board cards must have a name that is a link to a Trello board. Mirror cards must have a name that is a link to a Trello card.

## ward-kdl ops trello card edit

`PUT /cards/{id}`

Authorized by grant: can edit card. Not destructive.

Positional arguments (1):

- `<id>` (string)

Options (15):

- `--name` (string, optional): The new name for the card
- `--desc` (string, optional): The new description for the card
- `--closed` (boolean, optional): Whether the card should be archived (closed: true)
- `--idMembers` (string, optional): Comma-separated list of member IDs
- `--idAttachmentCover` (string, optional): The ID of the image attachment the card should use as its cover, or null for none
- `--idList` (string, optional): The ID of the list the card should be in
- `--idLabels` (string, optional): Comma-separated list of label IDs
- `--idBoard` (string, optional): The ID of the board the card should be on
- `--due` (string, optional): When the card is due, or `null`
- `--start` (string, optional): The start date of a card, or `null`
- `--dueComplete` (boolean, optional): Whether the status of the card is complete
- `--subscribed` (boolean, optional): Whether the member is should be subscribed to the card
- `--address` (string, optional): For use with/by the Map View
- `--locationName` (string, optional): For use with/by the Map View
- `--coordinates` (string, optional): For use with/by the Map View. Should be latitude,longitude

## ward-kdl ops trello card delete

`DELETE /cards/{id}`

Authorized by grant: can delete card. Destructive - mutates irreversibly.

Positional arguments (1):

- `<id>` (string)

## ward-kdl ops trello card comment

`POST /cards/{id}/actions/comments`

Authorized by grant: can comment card. Not destructive.

Positional arguments (1):

- `<id>` (string)

Options (1):

- `--text` (string, required): The comment

## Denied operations

### ward-kdl ops trello board delete (denied)

board deletion is irreversible; delete it in the Trello UI
