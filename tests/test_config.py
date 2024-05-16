import json
import jwt
import requests
import pytest
from requests import HTTPError

base_url = "https://tf-pocketbase.fly.dev"
base_url = "http://localhost:8080"


def expect(s):
    print(f"expect: {s}")


def jp(resp):
    print(resp.request.url)
    d = resp.json()
    print(json.dumps(d, indent=2))
    return d


@pytest.fixture
def owner_role():
    return "2arnubkcv7jpce8"


@pytest.fixture
def member_role():
    return "x6lllh2qsf9lxk6"


@pytest.fixture
def admin_session():
    resp = requests.post(f"{base_url}/api/admins/auth-with-password", json={
        "identity": "me@danielgk.com",
        "password": "3m=3)g;+*.iR7K]"
    })
    resp.raise_for_status()
    auth_token = resp.json()['token']
    unverified_claims = jwt.decode(auth_token, options={"verify_signature": False})
    #print(unverified_claims)
    session = requests.Session()
    session.headers = {
        "Authorization": f"Bearer {auth_token[:]}"
    }
    #print(auth_token)
    return session


@pytest.fixture
def user_session():
    # FIXME find a way to get this so it doesn't expire
    auth_token = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb2xsZWN0aW9uSWQiOiJfcGJfdXNlcnNfYXV0aF8iLCJleHAiOjE3MTY4NTczMTMsImlkIjoiYzVuOXhoMDg2OXptZnZ0IiwidHlwZSI6ImF1dGhSZWNvcmQifQ.GCb4Jqy9tMLwHQkAwBsbqx-wxM4tlOXYefJm37jGWII"
    unverified_claims = jwt.decode(auth_token, options={"verify_signature": False})
    #print(unverified_claims)
    session = requests.Session()
    session.headers = {
        "Authorization": f"Bearer {auth_token[:]}"
    }
    return session


@pytest.fixture
def user():
    # FIXME create for each test and tear down
    return "c5n9xh0869zmfvt"

@pytest.fixture
def member():
    # FIXME create for each test and tear down
    return "whdw32xb1f1s8er"

@pytest.fixture
def stranger():
    # FIXME create for each test and tear down
    return "bi9ig7vhydoi56w"


@pytest.fixture
def relay(user_session):
    resp = user_session.post(f"{base_url}/api/collections/relays/records/", json={"name": "Another Relay"})
    relay_id = resp.json()["id"]
    yield relay_id
    resp = user_session.delete(f"{base_url}/api/collections/relays/records/{relay_id}")


@pytest.fixture
def relay_with_member(user_session, relay, member, member_role):
    new_relay_role = {"relay": relay, "user": member, "role": member_role}
    expect("can create new relay role")
    resp = user_session.post(f"{base_url}/api/collections/relay_roles/records/", json=new_relay_role)
    jp(resp)
    yield relay
    resp = user_session.delete(f"{base_url}/api/collections/relays/records/{relay}")
 

def test_admin_session(admin_session, user):
    resp = admin_session.get(f"{base_url}/api/collections/users/records/{user}?expand=openai_apikey,anthropic_apikey,subscriptions_via_user")
    d = jp(resp)
    assert resp.status_code == 200


def test_user_read_self(user_session, user):
    expect("we can access ourselves, but not join on keys")
    resp = user_session.get(f"{base_url}/api/collections/users/records/{user}?expand=openai_apikey,anthropic_apikey,subscriptions_via_user")
    d = jp(resp)
    assert resp.status_code == 200
    assert "openai_apikey" not in d["expand"]
    assert "anthropic_apikey" not in d["expand"]


def test_user_read_other(user_session, stranger):
    expect("we can't access other guy")
    stranger = "bi9ig7vhydoi56w"
    resp = user_session.get(f"{base_url}/api/collections/users/records/{stranger}")
    d = jp(resp)
    assert resp.status_code == 404


def test_user_create_get_and_delete_relay(user_session, user):
    expect("we can create a relay")
    resp = user_session.post(f"{base_url}/api/collections/relays/records/", json={"name": "Another Relay"})
    jp(resp)
    relay = resp.json()['id']
    assert resp.status_code == 200
    expect("we can view the relay")
    resp = user_session.get(f"{base_url}/api/collections/relays/records/{relay}")
    jp(resp)
    assert resp.status_code == 200
    expect("we can delete the relay")
    resp = user_session.delete(f"{base_url}/api/collections/relays/records/{relay}")
    assert resp.status_code == 204


def test_user_read_roles(user_session, member_role, owner_role):
    expect("we can view roles (e.g. member, owner)")
    resp = user_session.get(f"{base_url}/api/collections/roles/records/{member_role}")
    jp(resp)
    assert resp.status_code == 200
    resp = user_session.get(f"{base_url}/api/collections/roles/records/{owner_role}")
    jp(resp)
    assert resp.status_code == 200


def test_view_relays_by_user(user_session, user):
    resp = user_session.get(f"{base_url}/api/collections/users/records/{user}?expand=relay_roles_via_user,relay_roles_via_user.relay,relay_roles_via_user.role")
    jp(resp)
    assert resp.status_code == 200
    

def test_owner_add_member(user_session, relay, member, member_role):
    new_relay_role = {"relay": relay, "user": member, "role": member_role}
    print(json.dumps(new_relay_role, indent=2))
    expect("can create new relay role")
    resp = user_session.post(f"{base_url}/api/collections/relay_roles/records/", json=new_relay_role)
    jp(resp)
    expect("can get role")
    relay_role = resp.json()['id']
    resp = user_session.get(f"{base_url}/api/collections/relay_roles/records/{relay_role}")
    jp(resp)


def test_owner_view_roles(user_session, member, member_role, relay_with_member):
    expect("can list relay roles")
    resp = user_session.get(f"{base_url}/api/collections/relay_roles/records/?expand=user,relay,role&filter=(relay='{relay_with_member}')")
    d = jp(resp)
    assert len(d["items"]) == 2


def test_owner_view_members(user_session, relay_with_member, member):
    expect("expect we can access member user info, because member on our relay")
    resp = user_session.get(f"{base_url}/api/collections/users/records/{member}")
    jp(resp)
    assert resp.status_code == 200


def test_owner_view_strangers(user_session, stranger):
    expect("expect we can't access stranger, because they aren't on any of our relays")
    resp = user_session.get(f"{base_url}/api/collections/users/records/{stranger}")
    jp(resp)
    assert resp.status_code == 404
