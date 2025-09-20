#!/usr/bin/env python3
"""
Test script for the AggregationService API
This demonstrates how to use the new DMTF Redfish standard AggregationService
"""

import json
import sys
import urllib.request
import urllib.parse
import urllib.error
from typing import Optional, Dict, Any


class AggregationServiceTester:
    def __init__(self, base_url: str = "http://localhost:8080"):
        self.base_url = base_url.rstrip("/")
        self.auth_token: Optional[str] = None
    
    def authenticate(self, username: str = "admin", password: str = "admin") -> bool:
        """Authenticate with the Redfish service."""
        print("1. Authenticating...")
        
        url = f"{self.base_url}/redfish/v1/SessionService/Sessions"
        data = json.dumps({
            "UserName": username,
            "Password": password
        }).encode('utf-8')
        
        try:
            req = urllib.request.Request(url, data=data, method='POST')
            req.add_header('Content-Type', 'application/json')
            
            with urllib.request.urlopen(req) as response:
                # Try to get token from header first
                self.auth_token = response.headers.get('X-Auth-Token')
                if not self.auth_token:
                    # Try to parse from response body
                    response_data = json.loads(response.read().decode('utf-8'))
                    self.auth_token = response_data.get('X-Auth-Token')
                
                if self.auth_token:
                    print("   ✓ Authenticated successfully")
                    return True
                else:
                    print("   ✗ Failed to get auth token")
                    return False
                    
        except urllib.error.HTTPError as e:
            print(f"   ✗ Authentication failed: {e.code} {e.reason}")
            if e.code == 401:
                print("   Make sure admin/admin credentials are valid")
            return False
        except Exception as e:
            print(f"   ✗ Failed to authenticate: {e}")
            print("   Make sure the service is running at", self.base_url)
            return False
    
    def make_request(self, path: str, method: str = 'GET', data: Optional[Dict] = None) -> Optional[Dict[str, Any]]:
        """Make an authenticated request to the API."""
        url = f"{self.base_url}{path}"
        
        request_data = None
        if data:
            request_data = json.dumps(data).encode('utf-8')
        
        try:
            req = urllib.request.Request(url, data=request_data, method=method)
            if self.auth_token:
                req.add_header('X-Auth-Token', self.auth_token)
            if request_data:
                req.add_header('Content-Type', 'application/json')
            
            with urllib.request.urlopen(req) as response:
                if method == 'DELETE' and response.status == 204:
                    return {}  # No content expected
                response_text = response.read().decode('utf-8')
                if response_text:
                    return json.loads(response_text)
                return {}
                
        except urllib.error.HTTPError as e:
            error_body = e.read().decode('utf-8')
            try:
                error_json = json.loads(error_body)
                return {"error": error_json, "status": e.code}
            except:
                return {"error": error_body, "status": e.code}
        except Exception as e:
            return {"error": str(e)}
    
    def check_service_root(self) -> bool:
        """Check if AggregationService is available in the service root."""
        print("\n2. Checking Service Root...")
        
        response = self.make_request("/redfish/v1/")
        if response and "AggregationService" in response:
            print("   ✓ AggregationService is available in Service Root")
            return True
        else:
            print("   ✗ AggregationService not found in Service Root")
            return False
    
    def get_aggregation_service(self) -> Optional[Dict]:
        """Get the AggregationService resource."""
        print("\n3. Getting AggregationService...")
        
        response = self.make_request("/redfish/v1/AggregationService")
        if response and "error" not in response:
            print("   Response:", json.dumps(response, indent=2))
            return response
        else:
            print("   ✗ Failed to get AggregationService:", response)
            return None
    
    def list_connection_methods(self) -> Optional[Dict]:
        """List current ConnectionMethods."""
        print("\n4. Listing current ConnectionMethods...")
        
        response = self.make_request("/redfish/v1/AggregationService/ConnectionMethods")
        if response and "error" not in response:
            print("   Response:", json.dumps(response, indent=2))
            return response
        else:
            print("   ✗ Failed to list ConnectionMethods:", response)
            return None
    
    def add_connection_method(self, name: str, address: str, username: str, password: str) -> Optional[Dict]:
        """Add a new ConnectionMethod."""
        print("\n5. Adding a new ConnectionMethod...")
        print(f"   Name: {name}")
        print(f"   Address: {address}")
        print("   Note: This will fail if the BMC is not reachable.")
        
        data = {
            "Name": name,
            "ConnectionMethodType": "Redfish",
            "ConnectionMethodVariant.Address": address,
            "ConnectionMethodVariant.Authentication": {
                "Username": username,
                "Password": password
            }
        }
        
        response = self.make_request(
            "/redfish/v1/AggregationService/ConnectionMethods",
            method='POST',
            data=data
        )
        
        if response and "error" not in response:
            print("   ✓ ConnectionMethod added successfully")
            print("   Response:", json.dumps(response, indent=2))
            return response
        else:
            print("   ✗ Failed to add ConnectionMethod:", response)
            return None
    
    def check_managers_collection(self) -> Optional[Dict]:
        """Check the aggregated Managers collection."""
        print("\n6. Checking aggregated Managers collection...")
        
        response = self.make_request("/redfish/v1/Managers")
        if response and "error" not in response:
            print(f"   Found {response.get('Members@odata.count', 0)} managers")
            print("   Response:", json.dumps(response, indent=2))
            return response
        else:
            print("   ✗ Failed to get Managers collection:", response)
            return None
    
    def check_systems_collection(self) -> Optional[Dict]:
        """Check the aggregated Systems collection."""
        print("\n7. Checking aggregated Systems collection...")
        
        response = self.make_request("/redfish/v1/Systems")
        if response and "error" not in response:
            print(f"   Found {response.get('Members@odata.count', 0)} systems")
            print("   Response:", json.dumps(response, indent=2))
            return response
        else:
            print("   ✗ Failed to get Systems collection:", response)
            return None
    
    def delete_connection_method(self, method_id: str) -> bool:
        """Delete a ConnectionMethod by ID."""
        print(f"\n8. Deleting ConnectionMethod {method_id}...")
        
        response = self.make_request(
            f"/redfish/v1/AggregationService/ConnectionMethods/{method_id}",
            method='DELETE'
        )
        
        if response is not None and "error" not in response:
            print(f"   ✓ ConnectionMethod {method_id} deleted successfully")
            return True
        else:
            print(f"   ✗ Failed to delete ConnectionMethod:", response)
            return False


def main():
    print("========================================")
    print("AggregationService API Test Script")
    print("========================================")
    
    # Parse command line arguments
    import argparse
    parser = argparse.ArgumentParser(description='Test the AggregationService API')
    parser.add_argument('--url', default='http://localhost:8080', 
                        help='Base URL of the Redfish service (default: http://localhost:8080)')
    parser.add_argument('--username', default='admin',
                        help='Username for authentication (default: admin)')
    parser.add_argument('--password', default='admin',
                        help='Password for authentication (default: admin)')
    parser.add_argument('--bmc-address', default='192.168.1.100',
                        help='BMC address to add as ConnectionMethod (default: 192.168.1.100)')
    parser.add_argument('--bmc-username', default='admin',
                        help='BMC username (default: admin)')
    parser.add_argument('--bmc-password', default='password',
                        help='BMC password (default: password)')
    parser.add_argument('--skip-add', action='store_true',
                        help='Skip adding a new ConnectionMethod (useful if BMC is not available)')
    
    args = parser.parse_args()
    
    # Create tester instance
    tester = AggregationServiceTester(args.url)
    
    # Run tests
    if not tester.authenticate(args.username, args.password):
        print("\n✗ Authentication failed. Cannot continue.")
        return 1
    
    tester.check_service_root()
    tester.get_aggregation_service()
    tester.list_connection_methods()
    
    if not args.skip_add:
        method = tester.add_connection_method(
            "Test BMC",
            args.bmc_address,
            args.bmc_username,
            args.bmc_password
        )
        
        # If we successfully added a method, try to delete it to clean up
        if method and "Id" in method:
            tester.delete_connection_method(method["Id"])
    
    tester.check_managers_collection()
    tester.check_systems_collection()
    
    print("\n========================================")
    print("Test completed!")
    print("========================================")
    return 0


if __name__ == "__main__":
    sys.exit(main())