routerAdd("POST", "/authn", (c) => {
    const data = new DynamicModel({
        token: "",
    })

    // read the request into the data variable
    c.bind(data)

    const record = $app.dao().findAuthRecordByToken(data.token, $app.settings().recordAuthToken.secret)
    $app.dao().expandRecord(record, ["user_settings_via_user.openai_apikey", "user_settings_via_user.anthropic_apikey", "relay_roles_via_user", "relay_roles_via_user.relay", "relay_roles_via_user.role"], null)
    return $apis.recordAuthResponse($app, c, record, null);
}, $apis.requireAdminAuth())
