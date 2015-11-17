angular.module('syncthingfuse.core').controller('SyncthingFuseController', function ($scope, $http) {
    $scope.config = { devices: [] };

    $http.get('/api/system/config').success(function(data) {
        $scope.config = data;
    });

    $scope.thisDevice = function () {
        for (var i = 0; i < $scope.config.devices.length; i++) {
            var device = $scope.config.devices[i];
            if (device.deviceID === $scope.config.myID) {
                return device;
            }
        }
    };

    $scope.otherDevices = function() {
        var devices = [];

        for (var i=0 ; i<$scope.config.devices.length ; i++) {
            device = $scope.config.devices[i];
            if (device.deviceID !== $scope.config.myID) {
                devices.push(device)
            }
        }

        return devices;
    };

    $scope.addDevice = function() {
        $scope.currentDevice = {
            deviceID: '',
            _addressesStr: 'dynamic',
            compression: 'metadata',
            introducer: false,
            selectedFolders: {}
        };
        $scope.editingExisting = false;
        $scope.deviceEditor.$setPristine();
        $('#editDevice').modal();
    };

    $scope.editDevice = function(device) {
        $scope.currentDevice = device
        $scope.editingExisting = true;
        $scope.currentDevice._addressesStr = device.addresses.join(', ');

        $scope.currentDevice.selectedFolders = {};
        for (var i=0 ; i<$scope.config.folders.length ; i++) {
            var folder = $scope.config.folders[i];
            for (var j=0 ; j<folder.devices.length ; j++) {
                if (folder.devices[j].deviceID === device.deviceID) {
                    $scope.currentDevice.selectedFolders[folder.id] = true;
                }
            }
        }

        $scope.deviceEditor.$setPristine();
        $('#editDevice').modal();
    };

    $scope.saveDevice = function() {
        $('#editDevice').modal('hide');

        var deviceCfg = $scope.currentDevice;

        if (false === $scope.editingExisting) {
            // add to devices
            $scope.config.devices.push(deviceCfg);
        }

        // edit device
        deviceCfg.addresses = deviceCfg._addressesStr.split(',').map(function (x) {
            return x.trim();
        });

        // manipulate folder configurations
        for (var i=0 ; i<$scope.config.folders.length ; i++) {
            var folder = $scope.config.folders[i];

            var j = folder.devices.findIndex(function(el) { return el.deviceID === deviceCfg.deviceID })

            if (j === -1 && deviceCfg.selectedFolders[folder.id]) {
                // device doesn't exist for folder, but should
                folder.devices.push({deviceID: deviceCfg.deviceID})
            }
            if (j !== -1 && false === deviceCfg.selectedFolders[folder.id]) {
                // device exists for folder, but shouldn't
                folder.devices.splice(j, 1)
            }
        }

        $scope.saveConfig();
    };

    $scope.deleteDevice = function() {
        $('#editDevice').modal('hide');

        var deviceCfg = $scope.currentDevice;

        // remove from shares
        for (var i=0 ; i<$scope.config.folders.length ; i++) {
            var folder = $scope.config.folders[i];
            var j = folder.devices.findIndex(function(el) { return el.deviceID === deviceCfg.deviceID });
            if (j !== -1) {
                // device exists for folder, but shouldn't
                folder.devices.splice(j, 1)
            }
        }

        // remove from devices
        var i = $scope.config.devices.findIndex(function(el) { return el.deviceID === deviceCfg.deviceID });
        $scope.config.devices.splice(i, 1)

        $scope.saveConfig();
    };

    $scope.editFolder = function(folderCfg) {
        $scope.currentFolder = angular.copy(folderCfg);
        $scope.editingExisting = true;
        $scope.folderEditor.$setPristine();
        $('#editFolder').modal();
    };

    $scope.saveFolder = function() {
        $('#editFolder').modal('hide');
        var folderCfg = $scope.currentFolder;

        var folders = [];
        for (var i=0 ; i<$scope.config.folders.length ; i++) {
            if ($scope.config.folders[i].id === folderCfg.id) {
                folders.push(folderCfg)
            } else {
                folders.push($scope.config.folders[i])
            }
        }
        $scope.config.folders = folders;

        $scope.saveConfig();
    };

    $scope.saveConfig = function () {
        var cfg = JSON.stringify($scope.config);
        var opts = {
            headers: {
                'Content-Type': 'application/json'
            }
        };
        $http.post('/api/system/config', cfg, opts).success(function () {
            console.log("saved config successfully"); // TODO show real message
        }).error($scope.emitHTTPError);
    };

    $scope.emitHTTPError = function (data, status, headers, config) {
        // TODO handle errors for serious
        $scope.$emit('HTTPError', {data: data, status: status, headers: headers, config: config});
    };
});
